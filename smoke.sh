#!/usr/bin/env bash
# End-to-end: sign a cover image, PUT the bytes straight to storage, read it back.
set -euo pipefail
BASE=${BASE:-http://localhost:8080}

printf 'cover-bytes' > /tmp/cover.png

resp=$(curl -sS -X POST "$BASE/assets/upload-url" \
  -H 'Content-Type: application/json' \
  -d '{"creator_id":"mara","product_id":"loop-kit-02","filename":"cover.png","content_type":"image/png","size_bytes":11}')
echo "$resp"

url=$(printf '%s' "$resp" | sed -n 's/.*"upload_url":"\([^"]*\)".*/\1/p')
key=$(printf '%s' "$resp" | sed -n 's/.*"key":"\([^"]*\)".*/\1/p')

curl -sS -X PUT "$url" -H 'Content-Type: image/png' --data-binary @/tmp/cover.png -o /dev/null
curl -sS "$BASE/assets/status?key=$key"
echo
