#!/bin/sh
set -eu

upstream_repo='https://github.com/CookSleep/gpt_image_playground.git'
upstream_ref="${GPT_IMAGE_PLAYGROUND_LATEST_REF:-main}"
resolved_sha=''
attempt=1
while [ "$attempt" -le 3 ]; do
  if remote_refs="$(git ls-remote "$upstream_repo" "refs/heads/$upstream_ref")"; then
    resolved_sha="$(printf '%s\n' "$remote_refs" | awk 'NR == 1 { print $1 }')"
    if [ -n "$resolved_sha" ]; then
      break
    fi
  fi
  if [ "$attempt" -lt 3 ]; then
    sleep $((attempt * 5))
  fi
  attempt=$((attempt + 1))
done

if [ -z "$resolved_sha" ]; then
  echo "Unable to resolve upstream branch: $upstream_ref" >&2
  exit 1
fi

echo "Building gpt_image_playground from $upstream_ref at $resolved_sha"
exec docker build --build-arg "GPT_IMAGE_PLAYGROUND_REF=$resolved_sha" "$@" .
