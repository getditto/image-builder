# This script makes AMIs public in all regions specified in the packer-manifest.json file.
#!/bin/bash
set -euo pipefail

# Check if packer-manifest.json exists in the expected location
if [ -f "./output/packer-manifest.json" ]; then
  PACKER_MANIFEST_PATH="./output/packer-manifest.json"
else
  echo "Searching for packer-manifest.json in the current directory..."
  # Search for the manifest file in the current directory tree
  PACKER_MANIFEST_PATH=$(find . -name "packer-manifest.json" | head -n 1)
  if [ -z "$PACKER_MANIFEST_PATH" ]; then
    echo "Error: packer-manifest.json file could not be found."
    exit 1
  fi
fi

echo "Using packer-manifest.json at $PACKER_MANIFEST_PATH"

# Initialize arrays to store regions and AMI IDs
REGIONS=()
AMI_IDS=()

# Iterate over each build entry in the manifest
jq -c '.builds[]' < "$PACKER_MANIFEST_PATH" | while read -r BUILD; do
  # Extract the artifact_id field (contains region:ami_id pairs)
  ARTIFACT=$(echo "$BUILD" | jq -r '.artifact_id')
  # Split artifact string by comma to get each region:ami_id pair
  IFS=',' read -ra ARTIFACT_PARTS <<< "$ARTIFACT"

  for PART in "${ARTIFACT_PARTS[@]}"; do
    # Split each pair into region and ami_id
    IFS=':' read -r PART_LEFT PART_RIGHT <<< "$PART"

    # Create a JSON entry for validation (optional)
    JSON_ENTRY="{\"$PART_LEFT\": \"$PART_RIGHT\"}"
    if echo "$JSON_ENTRY" | jq empty > /dev/null 2>&1; then
      # Append region and AMI ID to arrays
      REGIONS+=("$PART_LEFT")
      AMI_IDS+=("$PART_RIGHT")
    else
      echo "Warning: Invalid JSON entry: $JSON_ENTRY"
    fi
  done

  # Create a JSON object with regions and ami_ids arrays (for reference)
  FINAL_JSON=$(jq -n \
    --argjson regions "$(printf '%s\n' "${REGIONS[@]}" | jq -R . | jq -s .)" \
    --argjson ami_ids "$(printf '%s\n' "${AMI_IDS[@]}" | jq -R . | jq -s .)" \
    '{regions: $regions, ami_ids: $ami_ids}')

  # Loop through the regions and AMI IDs to make each AMI public
  for i in "${!REGIONS[@]}"; do
    REGION="${REGIONS[$i]}"
    AMI_ID="${AMI_IDS[$i]}"

    # Disable block public access for the AMI in the region
    echo "Disabling block public access for AMI $AMI_ID in region $REGION."
    aws ec2 disable-image-block-public-access --region "$REGION"

    # Make the AMI public in the specified region
    echo "Making AMI $AMI_ID public in region $REGION"
    aws ec2 modify-image-attribute --image-id "$AMI_ID" --launch-permission "Add=[{Group=all}]" --region "$REGION"
  done

  # Verify that all AMIs are public in their respective regions
  for i in "${!AMI_IDS[@]}"; do
    AMI_ID="${AMI_IDS[$i]}"
    REGION="${REGIONS[$i]}"
    PUBLIC_STATE=$(aws ec2 describe-images --region "$REGION" --image-ids "$AMI_ID" --query "Images[0].Public" --output text)
    if [ "$PUBLIC_STATE" != "True" ]; then
      echo "Error: AMI $AMI_ID in region $REGION is not public."
      exit 1
    fi
    echo "Verified: AMI $AMI_ID in region $REGION is public."
  done

done
