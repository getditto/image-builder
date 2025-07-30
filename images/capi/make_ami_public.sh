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


# Populate associative array: region -> ami_id
# Extract the last build entry from the manifest
LAST_BUILD=$(jq -c '.builds[-1]' < "$PACKER_MANIFEST_PATH")
ARTIFACT=$(echo "$LAST_BUILD" | jq -r '.artifact_id')
IFS=',' read -ra ARTIFACT_PARTS <<< "$ARTIFACT"
for PART in "${ARTIFACT_PARTS[@]}"; do
  # Split each part into region and ami_id
  IFS=':' read -r REGION AMI_ID <<< "$PART"
  if [[ -n "$REGION" && -n "$AMI_ID" ]]; then
    REGION_TO_AMI["$REGION"]="$AMI_ID"
  fi
done




# Now, iterate over the associative array
for REGION in "${!REGION_TO_AMI[@]}"; do
  AMI_ID="${REGION_TO_AMI[$REGION]}"
  echo "Disabling block public access for AMI $AMI_ID in region $REGION."
  # aws ec2 disable-image-block-public-access --region "$REGION"
  echo "Making AMI $AMI_ID public in region $REGION"
  # aws ec2 modify-image-attribute --image-id "$AMI_ID" --launch-permission "Add=[{Group=all}]" --region "$REGION"

  # Retry logic to verify AMI is public
  MAX_RETRIES=10
  RETRY_DELAY=20  # in seconds
  RETRY_COUNT=0
  while true; do
    PUBLIC_STATE=$(aws ec2 describe-images --region "$REGION" --image-ids "$AMI_ID" --query "Images[0].Public" --output text 2>&1) || AWS_ERROR=$?
    if [[ -n "${AWS_ERROR:-}" && "$AWS_ERROR" -ne 0 ]]; then
      echo "Fatal error checking status of AMI $AMI_ID in region $REGION: $PUBLIC_STATE"
      exit 1
    fi
    if [ "$PUBLIC_STATE" == "True" ]; then
      echo "Confirmed: AMI $AMI_ID in region $REGION is public."
      break
    fi
    RETRY_COUNT=$((RETRY_COUNT+1))
    if [ "$RETRY_COUNT" -ge "$MAX_RETRIES" ]; then
      echo "Error: Timed out waiting for AMI $AMI_ID in region $REGION to become public."
      exit 1
    fi
    echo "AMI $AMI_ID in region $REGION is not yet public. Retrying in $RETRY_DELAY seconds... (Attempt $RETRY_COUNT/$MAX_RETRIES)"
    sleep "$RETRY_DELAY"
    unset AWS_ERROR
  done
done
