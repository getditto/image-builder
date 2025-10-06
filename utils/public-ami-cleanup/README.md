# Public AMI Cleanup Tool

An interactive CLI tool for managing AMIs in AWS that start with the prefix `capa-ami-`, focusing on making public AMIs private.

## Features

- Fetches ALL AMIs (both public and private) with the `capa-ami-` prefix across configured regions
- Displays AMIs in a tree structure showing root AMIs (us-east-1) and their copies
- Shows proper hierarchy even when root AMIs are already private
- Real-time status updates as AMIs are made private
- Interactive selection with keyboard navigation
- Bulk selection of entire trees or all public AMIs
- Converts selected public AMIs to private without closing the app

## Prerequisites

- AWS credentials configured (`aws configure` or environment variables)
- Appropriate IAM permissions:
  - `ec2:DescribeImages`
  - `ec2:ModifyImageAttribute`

## Installation

```bash
go build -o ami-cleanup
```

## Usage

```bash
./ami-cleanup
```

## Controls

### Navigation
- **↑/↓** or **j/k**: Navigate through the list
- **PgUp/PgDn**: Scroll by page
- **g/G** or **Home/End**: Go to top/bottom of list

### Selection
- **Space/Enter**: Expand/collapse tree or toggle selection
- **s**: Toggle selection (without expanding/collapsing)
- **a**: Toggle selection of all items in current tree
- **A**: Toggle selection of ALL AMIs

### Tree Management
- **e**: Expand all trees
- **x**: Collapse all trees

### Actions
- **c**: Confirm selection and make AMIs private
- **h/?**: Toggle help display
- **q/Ctrl+C**: Quit without changes

## Regions

The tool searches for AMIs in the following regions:
- us-east-1 (primary/root)
- us-east-2
- us-west-1
- us-west-2
- eu-west-1
- eu-west-2
- eu-west-3
- eu-central-1
- ca-central-1
- ap-southeast-2

## How it Works

1. Fetches ALL AMIs (public and private) with the `capa-ami-` prefix from all configured regions
2. Identifies root AMIs (those in us-east-1) and groups their copies from other regions as children
3. Displays proper tree structure even when root AMIs are already private
4. Presents an interactive tree view showing status of each AMI (PUBLIC/PRIVATE)
5. Allows selection of public AMIs only
6. Upon confirmation, modifies the selected AMIs in the background to remove public access
7. Updates the display in real-time to show the new status

## Notes

- AMIs are grouped by their source relationship (copies are shown under their source AMI)
- Private root AMIs are displayed with their public children properly grouped underneath
- The tool only affects AMIs owned by your account
- Only PUBLIC AMIs can be selected and made private
- AMIs that are already PRIVATE are shown but cannot be selected
- The app remains open after making changes, allowing you to continue working
- Making an AMI private cannot be undone through this tool