#!/bin/bash

# This script makes AMIs public in all the regions specified in the packer-manifest.json file.

set -euo pipefail

# Check if packer-manifest.json exists in the expected location
if [ -f "./output/packer-manifest.json" ]; then
  PACKER_MANIFEST_PATH="./output/packer-manifest.json"
else
  echo "Searching for packer-manifest.json in the current directory..."
  PACKER_MANIFEST_PATH=$(find . -name "packer-manifest.json" | head -n 1)
  if [ -z "$PACKER_MANIFEST_PATH" ]; then
    echo "Error: packer-manifest.json file could not be found."
    exit 1
  fi
fi

echo "Using packer-manifest.json at $PACKER_MANIFEST_PATH"

# Supported explicitly by running script in bash
# Initialize an associative array to store region -> ami_id mappings
declare -A REGION_TO_AMI


# Populate associative array: region -> ami_id from the last build only
LAST_BUILD=$(jq -c '.builds[-1]' < "$PACKER_MANIFEST_PATH")
ARTIFACT=$(echo "$LAST_BUILD" | jq -r '.artifact_id')
IFS=',' read -ra ARTIFACT_PARTS <<< "$ARTIFACT"
echo "Artifact parts found: ${#ARTIFACT_PARTS[@]}"
for PART in "${ARTIFACT_PARTS[@]}"; do
  # Split each part into region and ami_id
  IFS=':' read -r REGION AMI_ID <<< "$PART"
  if [[ -n "$REGION" && -n "$AMI_ID" ]]; then
    REGION_TO_AMI["$REGION"]="$AMI_ID"
  fi
done



# # # Wait for the while loop to finish and export the associative array
# # # Bash associative arrays can't be exported directly, so we use a workaround
# # # Save the array to a temp file and source it
# TMPFILE=$(mktemp)
# declare -p REGION_TO_AMI > "$TMPFILE"
# source "$TMPFILE"
# rm "$TMPFILE"



# # Now, iterate over the associative array
for REGION in "${!REGION_TO_AMI[@]}"; do
  AMI_ID="${REGION_TO_AMI[$REGION]}"
  echo "Disabling block public access for AMI $AMI_ID in region $REGION."
  # aws ec2 disable-image-block-public-access --region "$REGION"
  echo "Making AMI $AMI_ID public in region $REGION"
  # aws ec2 modify-image-attribute --image-id "$AMI_ID" --launch-permission "Add=[{Group=all}]" --region "$REGION"
done

# # Verify that all AMIs are public in their respective regions
for REGION in "${!REGION_TO_AMI[@]}"; do
  AMI_ID="${REGION_TO_AMI[$REGION]}"
  PUBLIC_STATE=$(aws ec2 describe-images --region "$REGION" --image-ids "$AMI_ID" --query "Images[].Public" --output text)
  if [ "$PUBLIC_STATE" != "True" ]; then
    echo "Error: AMI $AMI_ID in region $REGION is not public."
    exit 1
  fi
  echo "Verified: AMI $AMI_ID in region $REGION is public."
done
