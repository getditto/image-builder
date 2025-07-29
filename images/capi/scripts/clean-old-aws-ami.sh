#!/bin/bash
set -euo pipefail

LIVE_VERSION="${LIVE_VERSION:-}"
ACCOUNT_ID="${ACCOUNT_ID:-}"
REGIONS="eu-west-3 eu-west-2 eu-west-1 ca-central-1 eu-central-1 us-east-1 us-east-2 us-west-1 us-west-2 ap-southeast-2"
AMI_NAME_FILTER="capa-ami-ubuntu-24.04"

if [[ -z "$LIVE_VERSION" || -z "$ACCOUNT_ID" ]]; then
    echo "LIVE_VERSION and ACCOUNT_ID environment variables must be set."
    exit 1
fi

echo "Looking for public AMIs with kubernetes_version less than $LIVE_VERSION and name containing '$AMI_NAME_FILTER' in regions: $REGIONS"

for REGION in $REGIONS; do
    echo "🔎 Checking region: $REGION"
    AMI_IDS=$(aws ec2 describe-images \
        --owners "$ACCOUNT_ID" \
        --filters "Name=tag:kubernetes_version,Values=*" "Name=name,Values=$AMI_NAME_FILTER*" \
        --region "$REGION" \
        --query "Images[?Public==\`true\`].{ID:ImageId,Version:Tags[?Key=='kubernetes_version']|[0].Value}" \
        --output json | jq -r '.[] | [.ID, .Version] | @tsv' | awk -v live="$LIVE_VERSION" '$2 < live { print $1 }')

    if [ -z "$AMI_IDS" ]; then
        echo "✅ No outdated public AMIs found in $REGION. Skipping."
        continue
    fi

    for ami in $AMI_IDS; do
        echo "🧹 Deregistering public AMI: $ami (older than $LIVE_VERSION) in $REGION"
        echo "Making public access to AMI: $ami in $REGION private"
        aws ec2 modify-image-attribute --image-id "$ami" --region "$REGION" --launch-permission "Remove=[{Group=all}]"
    done
done
