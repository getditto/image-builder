#!/bin/sh
# This script updates the kubernetes_semver and aws_profile fields in the ubuntu-2404.json file.
# It ensures the Kubernetes version and AWS profile are set as desired.

# Find the ubuntu-2404.json file in the images/capi/packer directory
UBUNTU_FILE=$(find ./images/capi/packer -name "ubuntu-2404.json" | head -n 1)

# Exit if the file is not found
if [ -z "$UBUNTU_FILE" ]; then
    echo "Error: ubuntu-2404.json file could not be found."
    exit 1
fi

echo "Using ubuntu-2404.json at $UBUNTU_FILE"

# Set the Kubernetes version, defaulting to 1.30.10 if not provided
K8S_VERSION="${K8S_VERSION:-1.30.10}"

echo "Adding kubernetes_semver: $K8S_VERSION to $UBUNTU_FILE"

# Update the kubernetes_semver field in the JSON file
jq --arg k8s_version "$K8S_VERSION" '.kubernetes_semver = $k8s_version' "$UBUNTU_FILE" > "$UBUNTU_FILE.tmp" && mv "$UBUNTU_FILE.tmp" "$UBUNTU_FILE"

# Get the AWS profile from the environment and from the JSON file
AWS_PROFILE_ENV="${AWS_PROFILE}"
AWS_PROFILE_FILE=$(jq -r '.aws_profile' "$UBUNTU_FILE")

# If the profiles do not match, update the JSON file to match the environment
if [ "$AWS_PROFILE_ENV" != "$AWS_PROFILE_FILE" ]; then
    echo "AWS profile mismatch. Updating aws_profile in $UBUNTU_FILE to match $AWS_PROFILE_ENV."
    jq --arg aws_profile "$AWS_PROFILE_ENV" '.aws_profile = $aws_profile' "$UBUNTU_FILE" > "$UBUNTU_FILE.tmp" && mv "$UBUNTU_FILE.tmp" "$UBUNTU_FILE"
else
    echo "AWS profile matches: $AWS_PROFILE_ENV"
fi