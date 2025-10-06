# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is an interactive CLI tool for managing AWS AMIs with the `capa-ami-` prefix. It provides a tree-based UI for viewing and converting public AMIs to private across multiple AWS regions. Built with Go using the Bubble Tea TUI framework.

## Build and Run

```bash
# Build the binary
go build -o ami-cleanup

# Run the tool
./ami-cleanup
```

## Architecture

### Core Components

**main.go** - Application entry point and AWS integration
- `fetchAMIs()`: Queries all regions for AMIs owned by the account with `capa-ami-` prefix
- `buildAMITrees()`: Constructs hierarchical relationships between root AMIs (us-east-1) and their regional copies
- `makeAMIsPrivate()`: Executes concurrent AWS API calls to modify AMI permissions (5 concurrent operations max)

**tui.go** - Bubble Tea terminal UI implementation
- `model`: Holds application state including trees, selections, viewport position, and update channels
- `listItem`: Flattened representation of tree structure for keyboard navigation
- Tree expansion/collapse is managed via `expanded` map keyed by tree index
- Selection state tracked via `selected` map keyed by `"region:ami-id"`

### State Management

The application uses the Bubble Tea pattern with message passing:
- `amiUpdateStartMsg/amiUpdateSuccessMsg/amiUpdateErrorMsg`: Track individual AMI update lifecycle
- `allUpdatesCompleteMsg`: Signals completion of batch operations
- Updates happen in background goroutines, sending messages to `updateChan` which the UI consumes via `listenForUpdates()`

### Tree Building Logic (main.go:178-278)

1. Root AMIs are identified as those in us-east-1 region (both public and private)
2. Children are matched to roots by:
   - Exact name matching
   - SourceAMI/source-ami tag lookup
   - AMI ID in description field parsing
   - Fuzzy name prefix matching as fallback
3. Orphans (AMIs with no parent) become single-node trees
4. Children sorted by region for consistent display

### AWS Regions

The tool queries these regions (defined in main.go:59-70):
- us-east-1 (primary/root region)
- us-east-2, us-west-1, us-west-2
- eu-west-1, eu-west-2, eu-west-3, eu-central-1
- ca-central-1, ap-southeast-2

## Key Behaviors

- Only PUBLIC AMIs can be selected and modified
- UI remains open after making changes (does not exit)
- Viewport management in tui.go:157-188 ensures cursor stays visible when navigating large lists
- Tree state persists after collapsing/expanding - selections are maintained
- Making an AMI private is irreversible through this tool
