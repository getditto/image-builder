#!/bin/bash
# Set strict error handling
set -euo pipefail

# Source shared functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "${SCRIPT_DIR}/functions.sh"

# Configuration
MAX_RESULTS=50  # Limit the number of Public AMIs returned per query to avoid excessive API calls or large result sets
TIMEOUT=30       # Timeout in seconds for AWS calls
AMI_NAME_FILTER="capa-ami-ubuntu-24.04"  # Default AMI name filter
REGIONS=(eu-west-3 eu-west-2 eu-west-1 ca-central-1 eu-central-1 us-east-1 us-east-2 us-west-1 us-west-2 ap-southeast-2)
LIVE_VERSION="${LIVE_VERSION:-1.31.8}"  # Default live version, can be overridden by user
ACCOUNT_ID="${ACCOUNT_ID:-}"  # AWS account ID, must be set by user
USE_PAGINATION="${USE_PAGINATION:-true}"  # Use pagination if set to true
# Main execution
main() {
    # Validate all inputs first
    validate_inputs

    # echo "Using AWS region: ${REGIONS[@]}"
    # Choose between limited or paginated approach
    if [[ "$USE_PAGINATION" == "true" ]]; then
        AMI_INFO=$(get_all_ami_info_paginated "${REGIONS[@]}" "$ACCOUNT_ID" "$AMI_NAME_FILTER" "$LIVE_VERSION" "$MAX_RESULTS")

    else
        AMI_INFO=$(get_ami_info "${REGIONS[@]}" "$ACCOUNT_ID" "$AMI_NAME_FILTER" "$LIVE_VERSION" "$MAX_RESULTS")
    fi

    # Validate we got some results
    if [[ -z "$AMI_INFO" ]]; then
        log "No AMIs found matching the criteria."
        exit 1
    else
        # Output results
        log "Successfully retrieved AMI information: $AMI_INFO"
    fi

}

# Run main function if script is executed directly
# This check ensures the script is not being sourced, but executed as the main program.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
