# gpt_image_playground integration

The New API integration uses the Fork [Arlenman/gpt_image_playground](https://github.com/Arlenman/gpt_image_playground) as the maintained upstream source. Its Git submodule is kept at `third_party/gpt-image-playground` and is always checked out at a detached, reviewed commit. The current validated commit is recorded in `upstream.commit`.

`build-latest.sh` is the maintenance helper for this integration. It:

1. Resolves `GPT_IMAGE_PLAYGROUND_LATEST_REF` (default: `main`) on the Fork to an immutable SHA.
2. Initializes the `third_party/gpt-image-playground` submodule, fetches that SHA, and checks it out detached.
3. Updates `upstream.commit` atomically.
4. Runs `patch-upstream.test.mjs` before invoking the Docker build with `GPT_IMAGE_PLAYGROUND_REF=<resolved-sha>`.

The helper never runs `git add`, `git commit`, or `git push`. It intentionally does not modify `.gitmodules`, `Dockerfile`, the bridge, or the upstream patch. Use it from the repository root:

```sh
tools/gpt-image-playground/build-latest.sh -t new-api:gpt-image-playground-latest
```

Set `GPT_IMAGE_PLAYGROUND_LATEST_REF` to another Fork branch when preparing an upgrade. Review the resulting submodule SHA and `upstream.commit` change, run the full integration checks, and commit them through the normal project review process.

`write-build-info.mjs` reads the committed `upstream.commit` marker rather than relying on the upstream checkout's `.git` metadata, so the generated `dist/build-info.json` remains deterministic in Docker build contexts without Git history.

`patch-upstream.mjs` fails the build if the validated upstream entry, service-worker, persistence, Agent response-output merge, image-execution, or retry markers drift. The integration disables the upstream offline service worker so authenticated tool pages cannot be served from an application-shell cache, filters both runtime-managed New API image/Agent profiles before upstream settings are written to localStorage, deduplicates repeated streamed Agent tool items by `type + call_id`, and executes distinct image calls concurrently. The bridge restores the upstream tool's previous active and Agent profile selections, while third-party profiles remain owned and persisted by the upstream tool.
