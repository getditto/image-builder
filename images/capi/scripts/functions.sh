# Function to log with timestamps
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >&2
}

# Function to validate inputs
validate_inputs() {
    if [[ -z "${REGIONS[*]:-}" ]]; then
        log "ERROR: REGIONS is not set or empty"
        return 1
    fi

    if [[ -z "${ACCOUNT_ID:-}" ]]; then
        log "ERROR: ACCOUNT_ID is not set"
        return 1
    fi

    if [[ -z "${AMI_NAME_FILTER:-}" ]]; then
        log "ERROR: AMI_NAME_FILTER is not set"
        return 1
    fi

    if [[ -z "${LIVE_VERSION:-}" ]]; then
        log "ERROR: LIVE_VERSION is not set"
        return 1
    fi

    # Validate each region format
    for region in "${REGIONS[@]}"; do
        if [[ ! "$region" =~ ^[a-z0-9-]+$ ]]; then
            log "ERROR: Invalid region format: $region"
            return 1
        fi
    done

    # Validate account ID format (12 digits)
    if [[ ! "$ACCOUNT_ID" =~ ^[0-9]{12}$ ]]; then
        log "ERROR: Invalid account ID format: $ACCOUNT_ID"
        return 1
    fi

    # Validate LIVE_VERSION format (e.g., 1.27.3)
    if [[ ! "$LIVE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        log "ERROR: Invalid LIVE_VERSION format: $LIVE_VERSION"
        return 1
    fi
}



# Function to compare semantic versions: returns true if $1 < $2
version_lt() {
    # Strip leading 'v' if present
    local ver1="${1#v}"
    local ver2="${2#v}"
    [ "$(printf '%s\n' "$ver1" "$ver2" | sort -V | head -n1)" = "$ver1" ] && [ "$ver1" != "$ver2" ]
}

# Function to get AMI information with pagination and limits
get_ami_info() {
    local regions=("${@:1:$(($#-4))}")
    local account_id="${@: -4:1}"
    local ami_name_filter="${@: -3:1}"
    local live_version="${@: -2:1}"
    local max_results="${@: -1}"
    local -a total_outdated_amis=()
    local total_outdated_count=0

    for region_item in "${regions[@]}"; do
        if [[ -z "$region_item" ]]; then
            continue
        fi

        log "🔎 Checking region: $region_item (max results: $max_results)"
        local outdated_count=0
        local -a outdated_amis=()


        # Get public AMIs
        local ami_data
        ami_data=$(timeout "$TIMEOUT" aws ec2 describe-images \
            --owners "$account_id" \
            --filters \
                "Name=tag:kubernetes_version,Values=*" \
                "Name=name,Values=${ami_name_filter}*" \
                "Name=state,Values=available" \
            --region "$region_item" \
            --output json | \
            jq -r "[ .Images[] |
                select(.Public == true and .ImageId != null) |
                {
                    ID: .ImageId,
                    Version: (.Tags[] | select(.Key == \"kubernetes_version\").Value),
                    Name: .Name,
                    CreationDate: .CreationDate
                }
            ] | .[0:$max_results]")

        # Log the actual count
        local total_amis
        total_amis=$(echo "$ami_data" | jq '. | length')
        log "Found $total_amis public AMI(s) in region $region_item"

        if [[ -z "$ami_data" || "$ami_data" == "null" ]]; then
            log "No public AMIs found matching criteria in region $region_item"
            continue
        fi

        # Iterate over AMIs
        while read -r ami_info; do
            local ami_id ami_version
            ami_id=$(echo "$ami_info" | jq -r '.ID')
            ami_version=$(echo "$ami_info" | jq -r '.Version')

            if [[ -z "$ami_id" || -z "$ami_version" ]]; then
                continue
            fi

            # Compare versions using semantic versioning
            if version_lt "$ami_version" "$live_version"; then
                local success
                # Uncomment to actually convert the AMI to private.
                log "AMI $ami_id (version $ami_version) is older than live version $live_version. Converting to private..."
                # The following AWS CLI call is commented out for testing purposes.
                #     timeout "$TIMEOUT" aws ec2 modify-image-attribute --image-id "$ami_id" --region "$region_item" --launch-permission "Remove=[{Group=all}]" 2>/dev/null
                if [[ $? -eq 0 ]]; then
                    success="true"
                    log "Successfully converted AMI $ami_id to private. Success variable set to $success."
                else
                    success="false"
                    log "Failed to convert AMI $ami_id to private."
                fi
                outdated_amis+=("$ami_id|$ami_version|$live_version|$success|$region_item")
                total_outdated_amis+=("$ami_id|$ami_version|$live_version|$success|$region_item")
                ((outdated_count++))
                ((total_outdated_count++))
            else
                log "AMI $ami_id (version $ami_version) is newer than or equal to live version $live_version. Skipping..."
            fi
        done < <(echo "$ami_data" | jq -c '.[]')

        if [[ "${#outdated_amis[@]}" -eq 0 ]]; then
            log "No outdated public AMIs found in $region_item."
        else
            log "Total outdated AMIs converted to private in $region_item: $outdated_count "
        fi

        #sleep 10
    done

        # Summary of converted AMIs
        if [[ "$total_outdated_count" -gt 0 ]]; then
            echo
            echo "Summary of AMIs converted to private:"
            printf "\n%-22s | %-15s | %-15s | %-7s | %-15s\n" "AMI_ID" "AMI_VERSION" "LIVE_VERSION" "SUCCESS" "REGION"
            printf -- "-----------------------+-----------------+-----------------+---------+-----------------\n"
            for entry in "${total_outdated_amis[@]}"; do
                IFS='|' read -r ami_id ami_version live_version success region_entry <<< "$entry"
                printf "%-22s | %-15s | %-15s | %-7s | %-15s\n" "$ami_id" "$ami_version" "$live_version" "$success" "$region_entry"
            done
            echo
            echo "Successfully processed $total_outdated_count outdated AMI(s) across all regions."
        fi
}

get_all_ami_info_paginated() {
    local regions=("${@:1:$(($#-4))}")
    local account_id="${@: -4:1}"
    local ami_name_filter="${@: -3:1}"
    local live_version="${@: -2:1}"
    local max_results="${@: -1}"
    local -a total_outdated_amis=()
    local total_outdated_count=0

    for region in "${regions[@]}"; do
        region="${region// /}"
        if [[ -z "$region" ]]; then
            continue
        fi

        log "🔎 Getting ALL AMIs in region: $region (using pagination)"
        local next_token=""
        local page_count=0
        local -a outdated_amis=()
        local outdated_count=0

        while :; do
            ((page_count++))
            log "Fetching page $page_count in region $region ..."

            local result

            if [[ -n "$next_token" ]]; then
                result=$(timeout "$TIMEOUT" aws ec2 describe-images \
                    --owners "$account_id" \
                    --filters \
                        "Name=tag:kubernetes_version,Values=*" \
                        "Name=name,Values=${ami_name_filter}*" \
                        "Name=state,Values=available" \
                    --region "$region" \
                    --starting-token "$next_token" \
                    --output json)
            else
                result=$(timeout "$TIMEOUT" aws ec2 describe-images \
                    --owners "$account_id" \
                    --filters \
                        "Name=tag:kubernetes_version,Values=*" \
                        "Name=name,Values=${ami_name_filter}*" \
                        "Name=state,Values=available" \
                    --region "$region" \
                    --output json)
            fi

            if [[ -z "$result" || "$result" == "null" ]]; then
                break
            fi

            # Process the results using jq to filter public AMIs and format output
            local processed_result
            processed_result=$(echo "$result" | jq -r "[ .Images[] |
                        select(.Public == true and .ImageId != null) |
                        {
                            ID: .ImageId,
                            Version: (.Tags[] | select(.Key == \"kubernetes_version\").Value),
                            Name: .Name,
                            CreationDate: .CreationDate
                        }
                    ] | .[0:$max_results]")

            while read -r ami_info; do
                local ami_id ami_version
                ami_id=$(echo "$ami_info" | jq -r '.ID')
                ami_version=$(echo "$ami_info" | jq -r '.Version')

                if [[ -z "$ami_id" || -z "$ami_version" ]]; then
                    continue
                fi

                if version_lt "$ami_version" "$live_version"; then
                    local success
                    log "AMI $ami_id (version $ami_version) is older than live version $live_version. Converting to private..."
                    # The following AWS CLI call is commented out for testing purposes.
                    # timeout "$TIMEOUT" aws ec2 modify-image-attribute --image-id "$ami_id" --region "$region" --launch-permission "Remove=[{Group=all}]" 2>/dev/null
                    if [[ $? -eq 0 ]]; then
                        success="true"
                        log "Successfully converted AMI $ami_id to private. Success variable set to $success."
                    else
                        success="false"
                        log "Failed to convert AMI $ami_id to private."
                    fi
                    outdated_amis+=("$ami_id|$ami_version|$live_version|$success|$region")
                    total_outdated_amis+=("$ami_id|$ami_version|$live_version|$success|$region")
                    ((outdated_count++))
                    ((total_outdated_count++))
                else
                log "AMI $ami_id (version $ami_version) is newer than or equal to live version $live_version. Skipping..."
                fi
            done < <(echo "$processed_result" | jq -c '.[]')

            # Pagination handling
            next_token=$(echo "$result" | jq -r 'if type=="object" and has("NextToken") and .NextToken != null and .NextToken != "" then .NextToken else empty end')
            if [[ -z "$next_token" ]]; then
                break
            fi

            log "Next token for page $page_count: $next_token"

            if [[ $page_count -gt 50 ]]; then
                log "ERROR: Too many pages ($page_count). Stopping to prevent infinite loop."
                break
            fi
        done

        if [[ "${#outdated_amis[@]}" -eq 0 ]]; then
            log "No outdated public AMIs found in $region."
        else
            log "Total outdated AMIs converted to private in $region: $outdated_count"
        fi
    done

        # Summary of converted AMIs
        if [[ "$total_outdated_count" -gt 0 ]]; then
            echo
            echo "Summary of AMIs converted to private:"
            printf "\n%-22s | %-15s | %-15s | %-7s | %-15s\n" "AMI_ID" "AMI_VERSION" "LIVE_VERSION" "SUCCESS" "REGION"
            printf -- "-----------------------+-----------------+-----------------+---------+-----------------\n"
            for entry in "${total_outdated_amis[@]}"; do
            IFS='|' read -r ami_id ami_version live_version success region_entry <<< "$entry"
            printf "%-22s | %-15s | %-15s | %-7s | %-15s\n" "$ami_id" "$ami_version" "$live_version" "$success" "$region_entry"
            done
            echo
            echo "Successfully processed $total_outdated_count outdated AMI(s) across all regions."
        fi



}
