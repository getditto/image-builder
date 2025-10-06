package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	trees        []AMITree
	cursor       int
	selected     map[string]bool
	expanded     map[string]bool
	quitting     bool
	config       aws.Config
	ctx          context.Context
	showHelp     bool
	hidePrivate  bool        // Hide private AMIs (except roots with public children)
	items        []listItem  // Flattened list of visible items for navigation
	viewport     int         // Starting index of visible items
	height       int         // Terminal height
	width        int         // Terminal width
	updating     bool        // Are we currently updating AMIs?
	updateStatus string      // Status message for updates
	updateChan   chan tea.Msg
	statusMsg    string      // General status message
	errorMsg     string      // Error message
}

type listItem struct {
	isTree   bool
	tree     *AMITree
	treeIdx  int
	ami      *AMI
	isChild  bool
	parentID string
	indent   int
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42"))

	cursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	treeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	regionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("33"))

	amiIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	publicStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	privateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("34")).
			Bold(true)

	updatingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	scrollStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	dateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))
)

func initialModel(trees []AMITree, cfg aws.Config, ctx context.Context) model {
	// Sort trees by name
	sort.Slice(trees, func(i, j int) bool {
		return trees[i].Root.Name < trees[j].Root.Name
	})

	m := model{
		trees:      trees,
		selected:   make(map[string]bool),
		expanded:   make(map[string]bool),
		config:     cfg,
		ctx:        ctx,
		showHelp:   true,
		height:     24, // Default height, will be updated
		width:      80, // Default width, will be updated
		updateChan: make(chan tea.Msg),
	}

	// Start with all trees collapsed
	m.rebuildItems()
	return m
}

func (m *model) rebuildItems() {
	m.items = []listItem{}

	for treeIdx, tree := range m.trees {
		// Check if tree has public children
		hasPublicChildren := false
		if m.hidePrivate {
			for _, child := range tree.Children {
				if child.Status == StatusPublic {
					hasPublicChildren = true
					break
				}
			}
		}

		// Skip private root AMIs unless they have public children
		if m.hidePrivate && tree.Root.Status == StatusPrivate && !hasPublicChildren {
			continue
		}

		// Add the root item
		m.items = append(m.items, listItem{
			isTree:  true,
			tree:    &m.trees[treeIdx],
			treeIdx: treeIdx,
			ami:     tree.Root,
			isChild: false,
			indent:  0,
		})

		// Add children if expanded
		treeID := fmt.Sprintf("%d", treeIdx)
		if m.expanded[treeID] && len(tree.Children) > 0 {
			for _, child := range tree.Children {
				// Skip private children when hidePrivate is enabled
				if m.hidePrivate && child.Status == StatusPrivate {
					continue
				}

				m.items = append(m.items, listItem{
					isTree:   false,
					ami:      child,
					isChild:  true,
					parentID: tree.Root.ID,
					indent:   1,
				})
			}
		}
	}
}

func (m *model) updateViewport() {
	// Calculate the available height for items
	headerLines := 7 // Increased for status messages
	helpLines := 1
	if m.showHelp {
		helpLines = 14
	}
	availableHeight := m.height - headerLines - helpLines - 2

	if availableHeight < 1 {
		availableHeight = 10 // Minimum visible items
	}

	// Ensure cursor is visible
	if m.cursor < m.viewport {
		m.viewport = m.cursor
	} else if m.cursor >= m.viewport+availableHeight {
		m.viewport = m.cursor - availableHeight + 1
	}

	// Ensure viewport doesn't go out of bounds
	maxViewport := len(m.items) - availableHeight
	if maxViewport < 0 {
		maxViewport = 0
	}
	if m.viewport > maxViewport {
		m.viewport = maxViewport
	}
	if m.viewport < 0 {
		m.viewport = 0
	}
}

func listenForUpdates(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		listenForUpdates(m.updateChan),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		m.updateViewport()

	case amiUpdateStartMsg:
		// Mark AMI as updating
		for i := range m.trees {
			if m.trees[i].Root.Region+":"+m.trees[i].Root.ID == msg.amiKey {
				m.trees[i].Root.Status = StatusUpdating
			}
			for j := range m.trees[i].Children {
				if m.trees[i].Children[j].Region+":"+m.trees[i].Children[j].ID == msg.amiKey {
					m.trees[i].Children[j].Status = StatusUpdating
				}
			}
		}
		return m, listenForUpdates(m.updateChan)

	case amiUpdateSuccessMsg:
		// Mark AMI as private
		for i := range m.trees {
			if m.trees[i].Root.Region+":"+m.trees[i].Root.ID == msg.amiKey {
				m.trees[i].Root.Status = StatusPrivate
				delete(m.selected, msg.amiKey)
			}
			for j := range m.trees[i].Children {
				if m.trees[i].Children[j].Region+":"+m.trees[i].Children[j].ID == msg.amiKey {
					m.trees[i].Children[j].Status = StatusPrivate
					delete(m.selected, msg.amiKey)
				}
			}
		}
		m.statusMsg = fmt.Sprintf("✓ Made %s private", msg.amiKey)
		return m, listenForUpdates(m.updateChan)

	case amiUpdateErrorMsg:
		// Mark AMI as error
		for i := range m.trees {
			if m.trees[i].Root.Region+":"+m.trees[i].Root.ID == msg.amiKey {
				m.trees[i].Root.Status = StatusError
				m.trees[i].Root.ErrorMsg = msg.err.Error()
			}
			for j := range m.trees[i].Children {
				if m.trees[i].Children[j].Region+":"+m.trees[i].Children[j].ID == msg.amiKey {
					m.trees[i].Children[j].Status = StatusError
					m.trees[i].Children[j].ErrorMsg = msg.err.Error()
				}
			}
		}
		m.errorMsg = fmt.Sprintf("✗ Failed to update %s: %v", msg.amiKey, msg.err)
		return m, listenForUpdates(m.updateChan)

	case allUpdatesCompleteMsg:
		m.updating = false
		m.updateStatus = "✓ All updates complete!"
		return m, nil

	case tea.KeyMsg:
		// Clear error message on any key press
		m.errorMsg = ""

		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 && !m.updating {
				m.cursor--
				m.updateViewport()
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 && !m.updating {
				m.cursor++
				m.updateViewport()
			}

		case "pgup":
			if !m.updating {
				pageSize := (m.height - 10) / 2
				m.cursor -= pageSize
				if m.cursor < 0 {
					m.cursor = 0
				}
				m.updateViewport()
			}

		case "pgdn":
			if !m.updating {
				pageSize := (m.height - 10) / 2
				m.cursor += pageSize
				if m.cursor >= len(m.items) {
					m.cursor = len(m.items) - 1
				}
				m.updateViewport()
			}

		case "home", "g":
			if !m.updating {
				m.cursor = 0
				m.updateViewport()
			}

		case "end", "G":
			if !m.updating {
				m.cursor = len(m.items) - 1
				m.updateViewport()
			}

		case " ", "enter":
			if m.cursor < len(m.items) && !m.updating {
				item := m.items[m.cursor]
				if item.isTree && len(item.tree.Children) > 0 {
					// Toggle expand/collapse
					treeID := fmt.Sprintf("%d", item.treeIdx)
					m.expanded[treeID] = !m.expanded[treeID]
					m.rebuildItems()
					// Adjust cursor if needed
					if m.cursor >= len(m.items) {
						m.cursor = len(m.items) - 1
					}
					m.updateViewport()
				} else if item.ami != nil && item.ami.Status == StatusPublic {
					// Toggle selection only for public AMIs
					key := fmt.Sprintf("%s:%s", item.ami.Region, item.ami.ID)
					if m.selected[key] {
						delete(m.selected, key)
					} else {
						m.selected[key] = true
					}
				}
			}

		case "s":
			// Toggle selection without expanding/collapsing
			if m.cursor < len(m.items) && !m.updating {
				item := m.items[m.cursor]
				if item.ami != nil && item.ami.Status == StatusPublic {
					key := fmt.Sprintf("%s:%s", item.ami.Region, item.ami.ID)
					if m.selected[key] {
						delete(m.selected, key)
					} else {
						m.selected[key] = true
					}
				}
			}

		case "a":
			// Select/deselect all items in current tree
			if m.cursor < len(m.items) && !m.updating {
				item := m.items[m.cursor]
				var tree *AMITree

				if item.isTree {
					tree = item.tree
				} else if item.isChild {
					// Find parent tree
					for i := range m.trees {
						if m.trees[i].Root.ID == item.parentID {
							tree = &m.trees[i]
							break
						}
					}
				}

				if tree != nil {
					// Check if all public AMIs are selected
					allSelected := true
					if tree.Root.Status == StatusPublic {
						rootKey := fmt.Sprintf("%s:%s", tree.Root.Region, tree.Root.ID)
						if !m.selected[rootKey] {
							allSelected = false
						}
					}
					for _, child := range tree.Children {
						if child.Status == StatusPublic {
							childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
							if !m.selected[childKey] {
								allSelected = false
								break
							}
						}
					}

					// Toggle
					if allSelected {
						if tree.Root.Status == StatusPublic {
							rootKey := fmt.Sprintf("%s:%s", tree.Root.Region, tree.Root.ID)
							delete(m.selected, rootKey)
						}
						for _, child := range tree.Children {
							if child.Status == StatusPublic {
								childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
								delete(m.selected, childKey)
							}
						}
					} else {
						if tree.Root.Status == StatusPublic {
							rootKey := fmt.Sprintf("%s:%s", tree.Root.Region, tree.Root.ID)
							m.selected[rootKey] = true
						}
						for _, child := range tree.Children {
							if child.Status == StatusPublic {
								childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
								m.selected[childKey] = true
							}
						}
					}
				}
			}

		case "e":
			// Expand all
			if !m.updating {
				for i := range m.trees {
					if len(m.trees[i].Children) > 0 {
						treeID := fmt.Sprintf("%d", i)
						m.expanded[treeID] = true
					}
				}
				m.rebuildItems()
				m.updateViewport()
			}

		case "x":
			// Collapse all
			if !m.updating {
				for i := range m.trees {
					treeID := fmt.Sprintf("%d", i)
					m.expanded[treeID] = false
				}
				m.rebuildItems()
				if m.cursor >= len(m.items) {
					m.cursor = len(m.items) - 1
				}
				m.updateViewport()
			}

		case "c":
			if len(m.selected) > 0 && !m.updating {
				// Start the update process
				m.updating = true
				m.updateStatus = fmt.Sprintf("Updating %d AMI(s)...", len(m.selected))
				m.statusMsg = ""

				// Collect selected AMI keys
				selectedKeys := make([]string, 0, len(m.selected))
				for key := range m.selected {
					selectedKeys = append(selectedKeys, key)
				}

				// Start background updates
				go makeAMIsPrivate(m.ctx, m.config, selectedKeys, m.updateChan)
				return m, listenForUpdates(m.updateChan)
			}

		case "p":
			if !m.updating {
				m.hidePrivate = !m.hidePrivate
				m.rebuildItems()
				// Adjust cursor if needed
				if m.cursor >= len(m.items) && len(m.items) > 0 {
					m.cursor = len(m.items) - 1
				}
				m.updateViewport()
			}

		case "h", "?":
			m.showHelp = !m.showHelp
			m.updateViewport()
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder

	// Header
	s.WriteString(titleStyle.Render("Public AMI Cleanup Tool"))
	s.WriteString("\n")

	totalAMIs := 0
	publicCount := 0
	privateCount := 0
	for _, tree := range m.trees {
		totalAMIs++
		switch tree.Root.Status {
		case StatusPublic:
			publicCount++
		case StatusPrivate:
			privateCount++
		}
		for _, child := range tree.Children {
			totalAMIs++
			switch child.Status {
			case StatusPublic:
				publicCount++
			case StatusPrivate:
				privateCount++
			}
		}
	}

	s.WriteString(fmt.Sprintf("Total: %d AMIs | Public: %d | Private: %d | Selected: %d\n",
		totalAMIs, publicCount, privateCount, len(m.selected)))

	// Status messages
	if m.updateStatus != "" {
		s.WriteString(statusStyle.Render(m.updateStatus))
		s.WriteString("\n")
	}
	if m.statusMsg != "" {
		s.WriteString(statusStyle.Render(m.statusMsg))
		s.WriteString("\n")
	}
	if m.errorMsg != "" {
		s.WriteString(errorStyle.Render(m.errorMsg))
		s.WriteString("\n")
	}

	// Show scroll position if needed
	if len(m.items) > 0 {
		scrollInfo := fmt.Sprintf("[%d-%d of %d]",
			m.viewport+1,
			min(m.viewport+m.getVisibleItemCount(), len(m.items)),
			len(m.items))
		s.WriteString(scrollStyle.Render(scrollInfo))
	}
	s.WriteString("\n\n")

	// Calculate visible range
	visibleCount := m.getVisibleItemCount()
	endIdx := m.viewport + visibleCount
	if endIdx > len(m.items) {
		endIdx = len(m.items)
	}

	// Display visible items
	for i := m.viewport; i < endIdx; i++ {
		item := m.items[i]
		isCurrent := i == m.cursor

		// Cursor indicator
		prefix := "  "
		if isCurrent {
			prefix = cursorStyle.Render("> ")
		}

		// Indentation
		indent := strings.Repeat("  ", item.indent)

		// Selection indicator
		selectIcon := "   "
		if item.ami != nil && item.ami.Status == StatusPublic {
			key := fmt.Sprintf("%s:%s", item.ami.Region, item.ami.ID)
			if m.selected[key] {
				selectIcon = selectedStyle.Render("[✓]")
			} else {
				selectIcon = "[ ]"
			}
		}

		// Status indicator
		statusStr := ""
		switch item.ami.Status {
		case StatusPublic:
			statusStr = publicStyle.Render("[PUBLIC]")
		case StatusPrivate:
			statusStr = privateStyle.Render("[PRIVATE]")
		case StatusUpdating:
			statusStr = updatingStyle.Render("[UPDATING...]")
		case StatusError:
			statusStr = errorStyle.Render("[ERROR]")
		}

		// Build the line
		var line string
		if item.isTree {
			// Tree root
			expandIcon := " "
			if len(item.tree.Children) > 0 {
				treeID := fmt.Sprintf("%d", item.treeIdx)
				if m.expanded[treeID] {
					expandIcon = "▼"
				} else {
					expandIcon = "▶"
				}
			}

			name := item.ami.Name
			maxNameLen := m.width - 85
			if maxNameLen < 20 {
				maxNameLen = 20
			}
			if len(name) > maxNameLen {
				name = name[:maxNameLen-3] + "..."
			}

			// Format date as YYYY-MM-DD HH:MM:SS
			dateStr := item.ami.CreatedDate.Format("2006-01-02 15:04:05")

			line = fmt.Sprintf("%s%s%s %s %s %s (%s) %-7s %s %s",
				prefix,
				indent,
				expandIcon,
				selectIcon,
				name,
				amiIDStyle.Render(item.ami.ID),
				regionStyle.Render(item.ami.Region),
				item.ami.Architecture,
				dateStyle.Render(dateStr),
				statusStr)

			if len(item.tree.Children) > 0 {
				publicCopies := 0
				privateCopies := 0
				for _, child := range item.tree.Children {
					if child.Status == StatusPublic {
						publicCopies++
					} else if child.Status == StatusPrivate {
						privateCopies++
					}
				}
				line += fmt.Sprintf(" (%d public, %d private)", publicCopies, privateCopies)
			}

			if item.ami.Status == StatusError && item.ami.ErrorMsg != "" {
				line += fmt.Sprintf(" %s", errorStyle.Render(item.ami.ErrorMsg))
			}
		} else {
			// Child AMI
			connector := "├─"
			// Check if this is the last child
			if item.parentID != "" {
				// Find parent tree and check if this is last child
				for _, tree := range m.trees {
					if tree.Root.ID == item.parentID {
						if len(tree.Children) > 0 {
							lastChild := tree.Children[len(tree.Children)-1]
							if lastChild.ID == item.ami.ID {
								connector = "└─"
							}
						}
						break
					}
				}
			}

			// Format date as YYYY-MM-DD HH:MM:SS
			dateStr := item.ami.CreatedDate.Format("2006-01-02 15:04:05")

			line = fmt.Sprintf("%s%s%s %s %s (%s) %-7s %s %s",
				prefix,
				indent,
				connector,
				selectIcon,
				amiIDStyle.Render(item.ami.ID),
				regionStyle.Render(item.ami.Region),
				item.ami.Architecture,
				dateStyle.Render(dateStr),
				statusStr)

			if item.ami.Status == StatusError && item.ami.ErrorMsg != "" {
				line += fmt.Sprintf(" %s", errorStyle.Render(item.ami.ErrorMsg))
			}
		}

		// Truncate line if too long
		if len(line) > m.width {
			line = line[:m.width-3] + "..."
		}

		s.WriteString(line)
		s.WriteString("\n")
	}

	// Add scroll indicators
	if m.viewport > 0 {
		s.WriteString(scrollStyle.Render("  ↑ more above\n"))
	}
	if endIdx < len(m.items) {
		s.WriteString(scrollStyle.Render("  ↓ more below\n"))
	}

	if m.showHelp {
		help := []string{
			"",
			"Controls:",
			"  ↑/↓ or j/k     : Navigate",
			"  PgUp/PgDn      : Scroll page",
			"  g/G or Home/End: Go to top/bottom",
			"  Space/Enter    : Expand/collapse tree or toggle selection",
			"  s              : Toggle selection (without expanding)",
			"  a              : Toggle all in current tree",
			"  e              : Expand all trees",
			"  x              : Collapse all trees",
			"  p              : Toggle hide private AMIs",
			"  c              : Confirm and make private",
			"  h/?            : Toggle help",
			"  q/Ctrl+C       : Quit",
		}
		if m.updating {
			help = append(help, "", updatingStyle.Render("Updates in progress..."))
		}
		s.WriteString(helpStyle.Render(strings.Join(help, "\n")))
	} else {
		s.WriteString(helpStyle.Render("\nPress h or ? for help"))
	}

	return s.String()
}

func (m model) getVisibleItemCount() int {
	// Calculate the available height for items
	headerLines := 7
	helpLines := 1
	scrollIndicators := 2
	if m.showHelp {
		helpLines = 16
	}
	availableHeight := m.height - headerLines - helpLines - scrollIndicators - 1

	if availableHeight < 1 {
		availableHeight = 10 // Minimum visible items
	}

	return availableHeight
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}