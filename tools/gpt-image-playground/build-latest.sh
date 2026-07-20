#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=${GPT_IMAGE_PLAYGROUND_REPO_ROOT:-$(CDPATH= cd -- "$script_dir/../.." && pwd)}
fork_repo='https://github.com/Arlenman/gpt_image_playground.git'
submodule_rel_path='third_party/gpt-image-playground'
submodule_path="$repo_root/$submodule_rel_path"
commit_path="$repo_root/tools/gpt-image-playground/upstream.commit"
upstream_ref=${GPT_IMAGE_PLAYGROUND_LATEST_REF:-main}
resolved_sha=''
attempt=1

while [ "$attempt" -le 3 ]; do
  if remote_refs=$(git ls-remote "$fork_repo" "refs/heads/$upstream_ref"); then
    resolved_sha=$(printf '%s\n' "$remote_refs" | awk 'NR == 1 { print $1 }')
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
  echo "Unable to resolve Fork branch: $upstream_ref" >&2
  exit 1
fi

if [ ! -d "$submodule_path" ]; then
  echo "Missing submodule worktree: $submodule_rel_path" >&2
  exit 1
fi

git -C "$repo_root" submodule update --init -- "$submodule_rel_path"
git -C "$submodule_path" fetch --depth 1 "$fork_repo" "$resolved_sha"
git -C "$submodule_path" checkout --detach "$resolved_sha"
checked_out_sha=$(git -C "$submodule_path" rev-parse HEAD)

if [ "$checked_out_sha" != "$resolved_sha" ]; then
  echo "Submodule checkout mismatch: expected $resolved_sha, got $checked_out_sha" >&2
  exit 1
fi

commit_tmp="$commit_path.tmp.$$"
trap 'rm -f "$commit_tmp"' EXIT HUP INT TERM
printf '%s\n' "$resolved_sha" > "$commit_tmp"
mv "$commit_tmp" "$commit_path"
trap - EXIT HUP INT TERM

echo "Updated $submodule_rel_path to detached $resolved_sha"
echo "Running GPT Image Playground compatibility tests"
(cd "$repo_root" && node --test tools/gpt-image-playground/patch-upstream.test.mjs)

echo "Building gpt_image_playground from Fork branch $upstream_ref at $resolved_sha"
cd "$repo_root"
exec docker build --build-arg "GPT_IMAGE_PLAYGROUND_REF=$resolved_sha" "$@" .
