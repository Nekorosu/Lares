#!/usr/bin/env bash
# Exercises a small invite-authenticated upload on an explicitly selected server.
# Requirements for this optional operator script: bash, curl, jq.
set -euo pipefail
: "${LARES_URL:?Set LARES_URL to your HTTPS server (or http://127.0.0.1:8090 locally)}"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
umask 077
curl --fail --silent --show-error -c "$work/cookies" "$LARES_URL/login" -o "$work/login.html"
csrf=$(awk '$6=="homeshare_csrf" {print $7}' "$work/cookies")
printf 'Invite code: '
IFS= read -r -s invite
printf '\n'
printf '%s' "$invite" | jq -Rs '{code:.,device_name:"curl smoke test"}' > "$work/invite.json"
unset invite
printf 'X-CSRF-Token: %s\n' "$csrf" > "$work/headers"
curl --fail --silent --show-error -b "$work/cookies" -c "$work/cookies" --header @"$work/headers" \
  --json @"$work/invite.json" "$LARES_URL/api/auth/invite/activate"
printf 'Lares smoke test\n' > "$work/payload"
size=$(wc -c < "$work/payload")
printf '{"filename":"smoke.txt","size":%d,"content_type":"text/plain","expiry_days":1}' "$size" > "$work/create.json"
curl --fail --silent --show-error -b "$work/cookies" --header @"$work/headers" \
  --json @"$work/create.json" "$LARES_URL/api/uploads" -o "$work/reserve.json"
id=$(jq -er .upload_id "$work/reserve.json")
jq -r '"X-Upload-Secret: "+.upload_secret' "$work/reserve.json" >> "$work/headers"
curl --fail --silent --show-error --head -b "$work/cookies" --header @"$work/headers" "$LARES_URL/api/uploads/$id"
curl --fail --silent --show-error -b "$work/cookies" --header @"$work/headers" \
  -X PATCH --data-binary @"$work/payload" "$LARES_URL/api/uploads/$id?offset=0"
curl --fail --silent --show-error -b "$work/cookies" --header @"$work/headers" \
  -X POST "$LARES_URL/api/uploads/$id/complete"
curl --fail --silent --show-error -b "$work/cookies" "$LARES_URL/api/files?q=smoke.txt" -o "$work/files.json"
file_id=$(jq -er '.[0].id' "$work/files.json")
curl --fail --silent --show-error -b "$work/cookies" "$LARES_URL/download/$file_id" -o "$work/download"
cmp "$work/payload" "$work/download"
curl --fail --silent --show-error -b "$work/cookies" -H 'Range: bytes=0-0' "$LARES_URL/download/$file_id"
curl --fail --silent --show-error -b "$work/cookies" "$LARES_URL/api/stats"
printf '\n%s\n' 'Small upload and Range verified. The test file expires in one day; revoke the smoke session in Devices.'
