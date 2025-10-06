package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
)

type AMI struct {
	ID          string
	Name        string
	Region      string
	CreatedDate time.Time
	IsPublic    bool
	CopiedFrom  string
}

type AMITree struct {
	Root     *AMI
	Children []*AMI
}

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

	fmt.Println("Fetching public AMIs across regions...")
	amis, err := fetchAMIs(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch AMIs: %v", err)
	}

	if len(amis) == 0 {
		fmt.Println("No public AMIs found with prefix 'capa-ami-'")
		return nil
	}

	trees := buildAMITrees(amis)

	p := tea.NewProgram(initialModel(trees, cfg))
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running program: %v", err)
	}

	if m, ok := finalModel.(model); ok && m.confirmed {
		return makeAMIsPrivate(ctx, cfg, m.selected)
	}

	return nil
}

func fetchAMIs(ctx context.Context, cfg aws.Config) ([]AMI, error) {
	var allAMIs []AMI

	for _, region := range regions {
		regionCfg := cfg.Copy()
		regionCfg.Region = region

		client := ec2.NewFromConfig(regionCfg)

		input := &ec2.DescribeImagesInput{
			Owners: []string{"self"},
			Filters: []types.Filter{
				{
					Name:   aws.String("name"),
					Values: []string{"capa-ami-*"},
				},
				{
					Name:   aws.String("is-public"),
					Values: []string{"true"},
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

			ami := AMI{
				ID:          aws.ToString(image.ImageId),
				Name:        aws.ToString(image.Name),
				Region:      region,
				CreatedDate: createdDate,
				IsPublic:    aws.ToBool(image.Public),
			}

			for _, tag := range image.Tags {
				if aws.ToString(tag.Key) == "SourceAMI" || aws.ToString(tag.Key) == "source-ami" {
					ami.CopiedFrom = aws.ToString(tag.Value)
					break
				}
			}

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
	amiByNameAndRegion := make(map[string]*AMI)
	orphans := []AMI{}

	// Build lookup map by AMI name and region
	for i := range amis {
		key := fmt.Sprintf("%s:%s", amis[i].Name, amis[i].Region)
		amiByNameAndRegion[key] = &amis[i]
	}

	// First, create trees for all us-east-1 AMIs (root AMIs)
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
					for name, tree := range trees {
						if tree.Root.ID == amis[i].CopiedFrom {
							tree.Children = append(tree.Children, &amis[i])
							found = true
							break
						}
						// Also check if the name matches with slight variations
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

func makeAMIsPrivate(ctx context.Context, cfg aws.Config, selectedAMIs map[string]bool) error {
	fmt.Println("\nMaking selected AMIs private...")

	amisByRegion := make(map[string][]string)

	for amiID := range selectedAMIs {
		parts := strings.Split(amiID, ":")
		if len(parts) == 2 {
			region := parts[0]
			id := parts[1]
			amisByRegion[region] = append(amisByRegion[region], id)
		}
	}

	for region, amiIDs := range amisByRegion {
		regionCfg := cfg.Copy()
		regionCfg.Region = region
		client := ec2.NewFromConfig(regionCfg)

		for _, amiID := range amiIDs {
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
				fmt.Printf("Failed to make AMI %s private in %s: %v\n", amiID, region, err)
			} else {
				fmt.Printf("✓ Made AMI %s private in %s\n", amiID, region)
			}
		}
	}

	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s Successfully made %d AMI(s) private.\n", green("✓"), len(selectedAMIs))
	return nil
}