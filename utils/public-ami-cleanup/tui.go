package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	trees      []AMITree
	cursor     int
	selected   map[string]bool
	expanded   map[string]bool
	confirmed  bool
	quitting   bool
	config     aws.Config
	showHelp   bool
	items      []listItem  // Flattened list of visible items for navigation
	viewport   int         // Starting index of visible items
	height     int         // Terminal height
	width      int         // Terminal width
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

	scrollStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	dateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
)

func initialModel(trees []AMITree, cfg aws.Config) model {
	// Sort trees by name
	sort.Slice(trees, func(i, j int) bool {
		return trees[i].Root.Name < trees[j].Root.Name
	})

	m := model{
		trees:    trees,
		selected: make(map[string]bool),
		expanded: make(map[string]bool),
		config:   cfg,
		showHelp: true,
		height:   24, // Default height, will be updated
		width:    80, // Default width, will be updated
	}

	// Auto-expand trees that have children
	for i, tree := range trees {
		if len(tree.Children) > 0 {
			treeID := fmt.Sprintf("%d", i)
			m.expanded[treeID] = true
		}
	}

	m.rebuildItems()
	return m
}

func (m *model) rebuildItems() {
	m.items = []listItem{}

	for treeIdx, tree := range m.trees {
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
	// Account for header (4 lines), help text, and padding
	headerLines := 5
	helpLines := 1
	if m.showHelp {
		helpLines = 11
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

func (m model) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		m.updateViewport()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.updateViewport()
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.updateViewport()
			}

		case "pgup":
			// Move up by half a page
			pageSize := (m.height - 10) / 2
			m.cursor -= pageSize
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.updateViewport()

		case "pgdn":
			// Move down by half a page
			pageSize := (m.height - 10) / 2
			m.cursor += pageSize
			if m.cursor >= len(m.items) {
				m.cursor = len(m.items) - 1
			}
			m.updateViewport()

		case "home", "g":
			m.cursor = 0
			m.updateViewport()

		case "end", "G":
			m.cursor = len(m.items) - 1
			m.updateViewport()

		case " ", "enter":
			if m.cursor < len(m.items) {
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
				} else if item.ami != nil {
					// Toggle selection
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
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				if item.ami != nil {
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
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				var tree *AMITree

				if item.isTree {
					tree = item.tree
				} else if item.isChild {
					// Find parent tree
					for _, t := range m.trees {
						if t.Root.ID == item.parentID {
							tree = &t
							break
						}
					}
				}

				if tree != nil {
					// Check if all are selected
					allSelected := true
					rootKey := fmt.Sprintf("%s:%s", tree.Root.Region, tree.Root.ID)
					if !m.selected[rootKey] {
						allSelected = false
					}
					for _, child := range tree.Children {
						childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
						if !m.selected[childKey] {
							allSelected = false
							break
						}
					}

					// Toggle
					if allSelected {
						delete(m.selected, rootKey)
						for _, child := range tree.Children {
							childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
							delete(m.selected, childKey)
						}
					} else {
						m.selected[rootKey] = true
						for _, child := range tree.Children {
							childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
							m.selected[childKey] = true
						}
					}
				}
			}

		case "A":
			// Select/deselect all AMIs
			allSelected := true
			for _, tree := range m.trees {
				rootKey := fmt.Sprintf("%s:%s", tree.Root.Region, tree.Root.ID)
				if !m.selected[rootKey] {
					allSelected = false
					break
				}
				for _, child := range tree.Children {
					childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
					if !m.selected[childKey] {
						allSelected = false
						break
					}
				}
			}

			if allSelected {
				m.selected = make(map[string]bool)
			} else {
				for _, tree := range m.trees {
					rootKey := fmt.Sprintf("%s:%s", tree.Root.Region, tree.Root.ID)
					m.selected[rootKey] = true
					for _, child := range tree.Children {
						childKey := fmt.Sprintf("%s:%s", child.Region, child.ID)
						m.selected[childKey] = true
					}
				}
			}

		case "e":
			// Expand all
			for i := range m.trees {
				if len(m.trees[i].Children) > 0 {
					treeID := fmt.Sprintf("%d", i)
					m.expanded[treeID] = true
				}
			}
			m.rebuildItems()
			m.updateViewport()

		case "x":
			// Collapse all
			for i := range m.trees {
				treeID := fmt.Sprintf("%d", i)
				m.expanded[treeID] = false
			}
			m.rebuildItems()
			if m.cursor >= len(m.items) {
				m.cursor = len(m.items) - 1
			}
			m.updateViewport()

		case "c":
			if len(m.selected) > 0 {
				m.confirmed = true
				return m, tea.Quit
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
	for _, tree := range m.trees {
		totalAMIs++
		totalAMIs += len(tree.Children)
	}

	s.WriteString(fmt.Sprintf("Found %d public AMIs with prefix 'capa-ami-'\n", totalAMIs))
	s.WriteString(fmt.Sprintf("Selected: %d AMIs\n", len(m.selected)))

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
		selectIcon := "[ ]"
		if item.ami != nil {
			key := fmt.Sprintf("%s:%s", item.ami.Region, item.ami.ID)
			if m.selected[key] {
				selectIcon = selectedStyle.Render("[✓]")
			}
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
			maxNameLen := m.width - 80
			if maxNameLen < 20 {
				maxNameLen = 20
			}
			if len(name) > maxNameLen {
				name = name[:maxNameLen-3] + "..."
			}

			// Format date as YYYY-MM-DD HH:MM:SS
			dateStr := item.ami.CreatedDate.Format("2006-01-02 15:04:05")

			line = fmt.Sprintf("%s%s%s %s %s %s (%s) %s %s",
				prefix,
				indent,
				expandIcon,
				selectIcon,
				name,
				amiIDStyle.Render(item.ami.ID),
				regionStyle.Render(item.ami.Region),
				dateStyle.Render(dateStr),
				publicStyle.Render("[PUBLIC]"))

			if len(item.tree.Children) > 0 {
				line += fmt.Sprintf(" (%d copies)", len(item.tree.Children))
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

			line = fmt.Sprintf("%s%s%s %s %s (%s) %s %s",
				prefix,
				indent,
				connector,
				selectIcon,
				amiIDStyle.Render(item.ami.ID),
				regionStyle.Render(item.ami.Region),
				dateStyle.Render(dateStr),
				publicStyle.Render("[PUBLIC]"))
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
			"  A              : Toggle all AMIs",
			"  e              : Expand all trees",
			"  x              : Collapse all trees",
			"  c              : Confirm and make private",
			"  h/?            : Toggle help",
			"  q/Ctrl+C       : Quit",
		}
		s.WriteString(helpStyle.Render(strings.Join(help, "\n")))
	} else {
		s.WriteString(helpStyle.Render("\nPress h or ? for help"))
	}

	return s.String()
}

func (m model) getVisibleItemCount() int {
	// Calculate the available height for items
	headerLines := 5
	helpLines := 1
	scrollIndicators := 2
	if m.showHelp {
		helpLines = 14
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