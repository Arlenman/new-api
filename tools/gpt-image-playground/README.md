# gpt_image_playground integration

The upstream source is intentionally not vendored into this repository. The Docker build fetches a selected upstream revision, applies narrow entry-point and persistence patches, builds the upstream application, and copies only its `dist` output into the final new-api image.

- Default validated revision: `a10477581b3d43ac98d39777e4445625a9db113d` (`0.7.0`)
- Override a pinned revision: `docker build --build-arg GPT_IMAGE_PLAYGROUND_REF=<commit-or-tag> .`
- Resolve the current upstream `main` to an immutable SHA before building: `tools/gpt-image-playground/build-latest.sh -t <image-name>`
- Runtime asset override: `GPT_IMAGE_PLAYGROUND_DIST=/absolute/path/to/dist`

`patch-upstream.mjs` fails the build if the validated upstream entry, service-worker, persistence, Agent response-output merge, image-execution, or retry markers drift. The integration disables the upstream offline service worker so authenticated tool pages cannot be served from an application-shell cache, filters both runtime-managed New API image/Agent profiles before upstream settings are written to localStorage, deduplicates repeated streamed Agent tool items by `type + call_id`, and executes distinct image calls concurrently. The bridge restores the upstream tool's previous active and Agent profile selections, while third-party profiles remain owned and persisted by the upstream tool.
