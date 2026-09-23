#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/imogi-openapi.XXXXXX")"
trap 'rm -rf -- "$temporary_dir"' EXIT

cd "$project_root"
npx --yes @redocly/cli@1.34.3 bundle openapi/openapi.yaml \
  --dereferenced --ext json \
  -o "$temporary_dir/bundled.json"

node scripts/normalize-openapi-codegen.mjs \
  "$temporary_dir/bundled.json" \
  "$temporary_dir/codegen.json"

go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.0 \
  -config oapi-codegen.yaml \
  "$temporary_dir/codegen.json"
