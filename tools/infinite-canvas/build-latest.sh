#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=${INFINITE_CANVAS_REPO_ROOT:-$(CDPATH= cd -- "$script_dir/../.." && pwd)}
source_root=${INFINITE_CANVAS_SOURCE_ROOT:-"$repo_root/third_party/infinite-canvas"}
commit_file=${INFINITE_CANVAS_COMMIT_FILE:-"$script_dir/upstream.commit"}
output_arg=${1:-${INFINITE_CANVAS_DIST_DIR:-"$repo_root/build/infinite-canvas/dist"}}
node_bin=${INFINITE_CANVAS_NODE_BIN:-node}
bun_bin=${INFINITE_CANVAS_BUN_BIN:-bun}
base_path=${INFINITE_CANVAS_BASE_PATH:-/_tools/infinite-canvas/}

if [ "$#" -gt 1 ]; then
  echo "usage: tools/infinite-canvas/build-latest.sh [output-dir]" >&2
  exit 2
fi

if [ ! -d "$source_root/web" ] || [ ! -f "$source_root/VERSION" ]; then
  echo "Missing Infinite Canvas Submodule source: $source_root" >&2
  exit 1
fi

if [ ! -f "$commit_file" ]; then
  echo "Missing pinned commit marker: $commit_file" >&2
  exit 1
fi

pinned_commit=$(cat "$commit_file")
case "$pinned_commit" in
  ''|*[!0-9a-f]*)
    echo "Invalid pinned Infinite Canvas commit: expected a 40-character lowercase SHA" >&2
    exit 1
    ;;
esac
if [ "${#pinned_commit}" -ne 40 ]; then
  echo "Invalid pinned Infinite Canvas commit: expected a 40-character lowercase SHA" >&2
  exit 1
fi

if [ -e "$source_root/.git" ]; then
  if ! command -v git >/dev/null 2>&1; then
    echo "Cannot verify Infinite Canvas Submodule without git" >&2
    exit 1
  fi
  checked_out_commit=$(git -C "$source_root" rev-parse HEAD)
  if [ "$checked_out_commit" != "$pinned_commit" ]; then
    echo "Infinite Canvas Submodule commit mismatch: expected $pinned_commit, got $checked_out_commit" >&2
    exit 1
  fi
  if [ -n "$(git -C "$source_root" status --porcelain)" ]; then
    echo "Infinite Canvas Submodule has uncommitted changes: $source_root" >&2
    exit 1
  fi
fi

output_parent=$(dirname -- "$output_arg")
output_name=$(basename -- "$output_arg")
if [ "$output_name" = "." ] || [ "$output_name" = "/" ]; then
  echo "Invalid output directory: $output_arg" >&2
  exit 1
fi
mkdir -p "$output_parent"
output_parent=$(CDPATH= cd -- "$output_parent" && pwd)
output_dir="$output_parent/$output_name"
output_stage="$output_parent/.$output_name.tmp.$$"
work_root=$(mktemp -d "${TMPDIR:-/tmp}/new-api-infinite-canvas.XXXXXX")
work_source="$work_root/source"

cleanup() {
  rm -rf "$work_root" "$output_stage"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$work_source"
cp -R "$source_root/." "$work_source/"
rm -rf "$work_source/.git" "$work_source/web/node_modules" "$work_source/web/dist"

"$node_bin" "$script_dir/patch-upstream.mjs" "$work_source"
(
  cd "$work_source/web"
  "$bun_bin" install
  VITE_BASE="$base_path" "$bun_bin" run build
)
"$node_bin" "$script_dir/write-build-info.mjs" "$work_source" "$work_source/web/dist" "$commit_file"

test -f "$work_source/web/dist/index.html"
test -f "$work_source/web/dist/build-info.json"

rm -rf "$output_stage"
cp -R "$work_source/web/dist" "$output_stage"
rm -rf "$output_dir"
mv "$output_stage" "$output_dir"

echo "Built Infinite Canvas Submodule $pinned_commit into $output_dir"
