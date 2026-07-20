# Infinite Canvas integration

New API builds the maintained Fork [Arlenman/infinite-canvas](https://github.com/Arlenman/infinite-canvas) from the fixed Git Submodule at `third_party/infinite-canvas`. The reviewed commit is duplicated in `upstream.commit`; a local build fails when the Submodule checkout and this marker do not match or when the Submodule has uncommitted changes.

`build-latest.sh` is the shared Docker/local artifact builder. Despite the legacy filename, it does not fetch a branch or mutate the Submodule. It:

1. Reads and validates the fixed SHA in `upstream.commit`.
2. Verifies the Submodule SHA and clean state when Git metadata is available. Docker copies normally omit `.git`, so the committed marker remains the immutable build input there.
3. Copies the Submodule into a disposable temporary directory and removes copied Git metadata, dependencies, and stale output.
4. Applies `patch-upstream.mjs` only to the temporary copy.
5. Runs `bun install` and the Vite production build with `VITE_BASE=/_tools/infinite-canvas/`.
6. Writes `dist/build-info.json` and atomically replaces the requested output directory.

The Submodule is never patched or installed in place. The generated build information contains only the public Fork URL, Submodule path, fixed commit, upstream version, and build timestamp. It does not inspect Git remotes, environment variables, API keys, or other runtime configuration.

Run a local build from the New API repository root:

```sh
tools/infinite-canvas/build-latest.sh
```

The default output is `build/infinite-canvas/dist`. An explicit output directory can be supplied for Docker stages or local packaging:

```sh
tools/infinite-canvas/build-latest.sh /tmp/infinite-canvas-dist
```

Docker can reuse the same script after copying `third_party/infinite-canvas` and `tools/infinite-canvas` into the build stage:

```sh
INFINITE_CANVAS_SOURCE_ROOT=/build/source \
  /build/integration/build-latest.sh /build/output
```

Supported environment overrides are:

- `INFINITE_CANVAS_REPO_ROOT`: repository root used by local/test callers.
- `INFINITE_CANVAS_SOURCE_ROOT`: fixed Submodule source directory.
- `INFINITE_CANVAS_COMMIT_FILE`: pinned commit marker; defaults to `upstream.commit` beside the script.
- `INFINITE_CANVAS_DIST_DIR`: default output when no positional output is supplied.
- `INFINITE_CANVAS_NODE_BIN` and `INFINITE_CANVAS_BUN_BIN`: executable paths for controlled build environments.
- `INFINITE_CANVAS_BASE_PATH`: Vite base path; defaults to `/_tools/infinite-canvas/`.
- `SOURCE_DATE_EPOCH`: standard reproducible-build timestamp used for `build-info.json` when set.

`bun install` intentionally runs without frozen-lockfile mode because the Fork currently carries both npm and Bun lockfiles that can require Bun reconciliation. Any lockfile changes occur only in the disposable copy.

To upgrade, update the Submodule to a reviewed commit from the Fork, update `upstream.commit` to the same full SHA, run the patch compatibility tests and this build, then review both Git changes together. The helper never runs `git add`, `git commit`, `git push`, or network fetches.
