ARG GPT_IMAGE_PLAYGROUND_REF=a10477581b3d43ac98d39777e4445625a9db113d

FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS image-playground-builder

ARG GPT_IMAGE_PLAYGROUND_REF
RUN apk add --no-cache git
WORKDIR /build
RUN git init -q source \
    && git -C source remote add origin https://github.com/CookSleep/gpt_image_playground.git \
    && for attempt in 1 2 3; do \
        if git -C source fetch --depth 1 origin "${GPT_IMAGE_PLAYGROUND_REF}"; then break; fi; \
        if [ "${attempt}" = 3 ]; then exit 1; fi; \
        sleep $((attempt * 5)); \
    done \
    && git -C source checkout --detach FETCH_HEAD
COPY tools/gpt-image-playground /build/integration
RUN node /build/integration/patch-upstream.mjs /build/source \
    && cd /build/source \
    && npm ci \
    && npm run build \
    && node /build/integration/write-build-info.mjs /build/source /build/source/dist

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder

WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install --frozen-lockfile
COPY ./web/default ./default
COPY ./VERSION /build/VERSION
RUN cd default && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat /build/VERSION) bun run build

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder-classic

WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install --filter ./classic --frozen-lockfile
COPY ./web/classic ./classic
COPY ./VERSION /build/VERSION
RUN cd classic && VITE_REACT_APP_VERSION=$(cat /build/VERSION) bun run build

FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder2
ENV GO111MODULE=on CGO_ENABLED=0

ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder /build/web/default/dist ./web/default/dist
COPY --from=builder-classic /build/web/classic/dist ./web/classic/dist
RUN go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/
COPY --from=image-playground-builder /build/source/LICENSE /licenses/gpt-image-playground-LICENSE
COPY --from=image-playground-builder /build/source/dist /opt/new-api/tools/gpt-image-playground
ENV GPT_IMAGE_PLAYGROUND_DIST=/opt/new-api/tools/gpt-image-playground
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
