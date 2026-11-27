#!/usr/bin/env bash
set -euo pipefail

source_sha=$(git rev-parse --verify HEAD)
if [[ "$source_sha" != "$GITHUB_SHA" ]]; then
    echo "::error::The checkout does not match the CI source SHA." >&2
    exit 1
fi

entrypoints=$(git ls-tree --name-only "$source_sha" -- \
    cmd/identity/main.go cmd/workspace/main.go cmd/environments/main.go)
if [[ -z "$entrypoints" ]]; then
    kind=monolith
elif [[ $(wc -l <<< "$entrypoints") == 3 ]]; then
    kind=services
else
    echo "::error::Expected all three microservice entrypoints in the same commit." >&2
    exit 1
fi
printf 'kind=%s\n' "$kind" >> "$GITHUB_OUTPUT"
