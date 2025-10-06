package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	tea "github.com/charmbracelet/bubbletea"
)

type AMIStatus string

const (
	StatusPublic   AMIStatus = "PUBLIC"
	StatusPrivate  AMIStatus = "PRIVATE"
	StatusUpdating AMIStatus = "UPDATING"
	StatusError    AMIStatus = "ERROR"
)

type AMI struct {
	ID           string
	Name         string
	Region       string
	CreatedDate  time.Time
	Status       AMIStatus
	CopiedFrom   string
	ErrorMsg     string
	Architecture string
}

type AMITree struct {
	Root     *AMI
	Children []*AMI
}

// Messages for tea
type amiUpdateStartMsg struct {
	amiKey string
}

type amiUpdateSuccessMsg struct {
	amiKey string
}

type amiUpdateErrorMsg struct {
	amiKey string
	err    error
}

type allUpdatesCompleteMsg struct{}

var regions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-central-1",
	"ca-central-1",
	"ap-southeast-2",
}

func main() {
	if err := runApp(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runApp() error {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %v", err)
	}

	fmt.Println("Fetching AMIs across regions...")
	amis, err := fetchAMIs(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch AMIs: %v", err)
	}

	if len(amis) == 0 {
		fmt.Println("No AMIs found with prefix 'capa-ami-'")
		return nil
	}

	trees := buildAMITrees(amis)

	p := tea.NewProgram(initialModel(trees, cfg, ctx))
	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("error running program: %v", err)
	}

	return nil
}

func fetchAMIs(ctx context.Context, cfg aws.Config) ([]AMI, error) {
	var allAMIs []AMI

	for _, region := range regions {
		regionCfg := cfg.Copy()
		regionCfg.Region = region

		client := ec2.NewFromConfig(regionCfg)

		// Fetch ALL AMIs with the capa-ami- prefix (both public and private)
		input := &ec2.DescribeImagesInput{
			Owners: []string{"self"},
			Filters: []types.Filter{
				{
					Name:   aws.String("name"),
					Values: []string{"capa-ami-*"},
				},
			},
		}

		result, err := client.DescribeImages(ctx, input)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch AMIs in %s: %v\n", region, err)
			continue
		}

		for _, image := range result.Images {
			createdDate, _ := time.Parse(time.RFC3339, aws.ToString(image.CreationDate))

			// Determine if AMI is public or private
			status := StatusPrivate
			if aws.ToBool(image.Public) {
				status = StatusPublic
			}

			ami := AMI{
				ID:           aws.ToString(image.ImageId),
				Name:         aws.ToString(image.Name),
				Region:       region,
				CreatedDate:  createdDate,
				Status:       status,
				Architecture: string(image.Architecture),
			}

			// Try to find source AMI from tags
			for _, tag := range image.Tags {
				if aws.ToString(tag.Key) == "SourceAMI" || aws.ToString(tag.Key) == "source-ami" {
					ami.CopiedFrom = aws.ToString(tag.Value)
					break
				}
			}

			// If no tag, try to extract from description
			if ami.CopiedFrom == "" && strings.Contains(aws.ToString(image.Description), "ami-") {
				parts := strings.Split(aws.ToString(image.Description), " ")
				for _, part := range parts {
					if strings.HasPrefix(part, "ami-") {
						ami.CopiedFrom = part
						break
					}
				}
			}

			allAMIs = append(allAMIs, ami)
		}
	}

	return allAMIs, nil
}

func buildAMITrees(amis []AMI) []AMITree {
	trees := make(map[string]*AMITree)
	amiByID := make(map[string]*AMI)
	amiByNameAndRegion := make(map[string]*AMI)
	orphans := []AMI{}

	// Build lookup maps
	for i := range amis {
		amiByID[amis[i].ID] = &amis[i]
		key := fmt.Sprintf("%s:%s", amis[i].Name, amis[i].Region)
		amiByNameAndRegion[key] = &amis[i]
	}

	// First, create trees for ALL us-east-1 AMIs (root AMIs) - both public and private
	for i := range amis {
		if amis[i].Region == "us-east-1" {
			trees[amis[i].Name] = &AMITree{
				Root:     &amis[i],
				Children: []*AMI{},
			}
		}
	}

	// Then, add children to the trees
	for i := range amis {
		if amis[i].Region != "us-east-1" {
			// Try to find the parent tree by matching AMI name
			if tree, exists := trees[amis[i].Name]; exists {
				tree.Children = append(tree.Children, &amis[i])
			} else {
				// No matching root found, check if it's linked by CopiedFrom
				found := false
				if amis[i].CopiedFrom != "" {
					// Try to find tree by ID
					for _, tree := range trees {
						if tree.Root.ID == amis[i].CopiedFrom {
							tree.Children = append(tree.Children, &amis[i])
							found = true
							break
						}
					}

					// If not found in trees, check if CopiedFrom points to another region's AMI
					if !found {
						if sourceAMI, exists := amiByID[amis[i].CopiedFrom]; exists {
							// Check if there's a tree with the same name as the source
							if tree, treeExists := trees[sourceAMI.Name]; treeExists {
								tree.Children = append(tree.Children, &amis[i])
								found = true
							}
						}
					}
				}

				// Try name matching with variations as a last resort
				if !found {
					for name, tree := range trees {
						if strings.HasPrefix(amis[i].Name, name) || strings.HasPrefix(name, amis[i].Name) {
							tree.Children = append(tree.Children, &amis[i])
							found = true
							break
						}
					}
				}

				if !found {
					// Still no match, treat as orphan
					orphans = append(orphans, amis[i])
				}
			}
		}
	}

	// Sort children by region for better display
	for _, tree := range trees {
		sort.Slice(tree.Children, func(i, j int) bool {
			return tree.Children[i].Region < tree.Children[j].Region
		})
	}

	// Convert map to slice
	var result []AMITree
	for _, tree := range trees {
		result = append(result, *tree)
	}

	// Add orphans as single-node trees
	for _, orphan := range orphans {
		result = append(result, AMITree{
			Root:     &orphan,
			Children: []*AMI{},
		})
	}

	// Sort trees by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Root.Name < result[j].Root.Name
	})

	return result
}

func makeAMIsPrivate(ctx context.Context, cfg aws.Config, selectedAMIs []string, updateChan chan<- tea.Msg) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // Limit concurrent updates

	for _, amiKey := range selectedAMIs {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			parts := strings.Split(key, ":")
			if len(parts) != 2 {
				updateChan <- amiUpdateErrorMsg{
					amiKey: key,
					err:    fmt.Errorf("invalid AMI key format"),
				}
				return
			}

			region := parts[0]
			amiID := parts[1]

			// Send start message
			updateChan <- amiUpdateStartMsg{amiKey: key}

			// Make the API call
			regionCfg := cfg.Copy()
			regionCfg.Region = region
			client := ec2.NewFromConfig(regionCfg)

			input := &ec2.ModifyImageAttributeInput{
				ImageId: aws.String(amiID),
				LaunchPermission: &types.LaunchPermissionModifications{
					Remove: []types.LaunchPermission{
						{
							Group: types.PermissionGroupAll,
						},
					},
				},
			}

			_, err := client.ModifyImageAttribute(ctx, input)
			if err != nil {
				updateChan <- amiUpdateErrorMsg{
					amiKey: key,
					err:    err,
				}
			} else {
				updateChan <- amiUpdateSuccessMsg{amiKey: key}
			}
		}(amiKey)
	}

	// Wait for all updates to complete
	go func() {
		wg.Wait()
		updateChan <- allUpdatesCompleteMsg{}
	}()
}

func refreshAMIStatus(ctx context.Context, cfg aws.Config, trees []AMITree) {
	// This function can be used to refresh AMI status from AWS
	// For now, we'll rely on our local state updates
	// In a production app, you might want to periodically refresh from AWS
}