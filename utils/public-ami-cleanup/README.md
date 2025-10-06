# Public AMI Cleanup Tool

An interactive CLI tool for managing public AMIs in AWS that start with the prefix `capa-ami-`.

## Features

- Fetches all public AMIs with the `capa-ami-` prefix across configured regions
- Displays AMIs in a tree structure showing root AMIs (us-east-1) and their copies
- Interactive selection with keyboard navigation
- Bulk selection of entire trees or all AMIs
- Converts selected public AMIs to private

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

1. Fetches all public AMIs with the `capa-ami-` prefix from all configured regions
2. Identifies root AMIs (those in us-east-1) and their copies in other regions
3. Presents an interactive tree view for selection
4. Upon confirmation, modifies the selected AMIs to remove public access

## Notes

- AMIs are grouped by their source relationship (copies are shown under their source AMI)
- The tool only affects AMIs owned by your account
- Making an AMI private cannot be undone through this tool