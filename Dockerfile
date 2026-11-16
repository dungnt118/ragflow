ARG TARGETARCH

# Multi-arch runner base images. The amd64 build reuses the published
# infiniflow/github_action_runner:latest; the arm64 build uses
# myfbee/github-runner:arm64 (built from the github-runner-dockerfile for
# linux/arm64). Selecting via TARGETARCH lets a single Dockerfile build natively
# on either platform (or as one multi-arch image with
# --platform linux/amd64,linux/arm64). BuildKit only resolves the alias stage
# that matches the target platform, so a single-arch build never requires both
# runner images to be present.
FROM infiniflow/github_action_runner:latest      AS runner-amd64
FROM infiniflow/github_action_runner:latest-arm64 AS runner-arm64

# base stage
FROM ubuntu:24.04 AS base
USER root
SHELL ["/bin/bash", "-c"]

ARG NEED_MIRROR=0

#Optional parameter
#If set NEED_MIRROR=1, and set GITEE_TOKEN="xxxxx" , donwload source from gitee.
#If don't set GITEE_TOKEN , download from github
ARG GITEE_TOKEN

WORKDIR /ragflow

# copy models downloaded via download_deps.py
# layout.laws/manual/paper.onnx are byte-identical to layout.onnx, so we
# exclude them from the tar extract and symlink them to layout.onnx instead,
# saving ~219MB in the image.
RUN mkdir -p /ragflow/internal/rag/res/deepdoc /root/.ragflow
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/huggingface.co,target=/huggingface.co \
    tar --exclude='.*' \
        --exclude='layout.laws.onnx' \
        --exclude='layout.manual.onnx' \
        --exclude='layout.paper.onnx' \
        --exclude='layout.onnx' \
        --exclude='det.onnx' \
        --exclude='rec.onnx' \
        --exclude='tsr.onnx' \
        --exclude='layout.laws.ort' \
        --exclude='layout.manual.ort' \
        --exclude='layout.paper.ort' \
        -cf - \
        /huggingface.co/InfiniFlow/deepdoc \
        | tar -xf - --strip-components=3 -C /ragflow/internal/rag/res/deepdoc && \
    #ln -s layout.onnx /ragflow/internal/rag/res/deepdoc/layout.laws.onnx && \
    #ln -s layout.onnx /ragflow/internal/rag/res/deepdoc/layout.manual.onnx && \
    #ln -s layout.onnx /ragflow/internal/rag/res/deepdoc/layout.paper.onnx
    ln -s layout.ort /ragflow/internal/rag/res/deepdoc/layout.laws.ort && \
    ln -s layout.ort /ragflow/internal/rag/res/deepdoc/layout.manual.ort && \
    ln -s layout.ort /ragflow/internal/rag/res/deepdoc/layout.paper.ort

# Copy the cl100k_base BPE table used by the Go tokenizer (tiktoken-go
# cl100k_base). The deps image ships it at its root; the Go image previously
# mounted only /huggingface.co and omitted this file, so NumTokensFromString
# silently returned 0. localBpeLoader resolves it from <workdir>/deps/,
# so dropping it here makes the table load offline at startup (see
# InitCL100KEncoder fail-fast guard).
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/cl100k_base.tiktoken,target=/tmp/cl100k_base.tiktoken \
    mkdir -p /ragflow/deps && \
    cp /tmp/cl100k_base.tiktoken /ragflow/deps/cl100k_base.tiktoken

# Embedding tokenizer assets for the Go counters (internal/tokenizer). Same story as
# the cl100k table above: the deps image carries them and the Go image has to put them
# where the loaders search (<workdir>/deps/huggingface.co/<repo>/<file>). A
# missing asset FAILS THE BUILD here rather than shipping an image where every tagged
# model silently counts with the calibrated cl100k estimate. The tokenizer.json files
# that exist only as cross-check oracles are deliberately not shipped.
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/huggingface.co,target=/huggingface.co \
    for asset in \
        BAAI/bge-m3/sentencepiece.bpe.model \
        BAAI/bge-large-en-v1.5/vocab.txt \
        Qwen/Qwen3-Embedding-0.6B/tokenizer.json \
        intfloat/e5-mistral-7b-instruct/tokenizer.json ; do \
        if [ -f "/huggingface.co/$asset" ]; then \
            mkdir -p "/ragflow/deps/huggingface.co/$(dirname "$asset")" && \
            cp "/huggingface.co/$asset" "/ragflow/deps/huggingface.co/$asset" ; \
        else \
            echo "ERROR: tokenizer asset $asset is missing from the infiniflow/ragflow_deps image; this image would count with the calibrated estimate instead of the model's own tokenizer" >&2 ; \
            exit 1 ; \
        fi ; \
    done

ENV DEBIAN_FRONTEND=noninteractive

# Setup apt
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    if [ "$NEED_MIRROR" == "1" ]; then \
        # CI runners may inject a proxy whose TLS certificate is not trusted inside
        # the fresh Ubuntu base image yet. Keep the Ubuntu mirror on HTTP here so
        # the mirror switch remains usable before the full CA store is available.
        sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i 's|http://security.ubuntu.com/ubuntu|http://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list.d/ubuntu.sources; \
    fi; \
    rm -f /etc/apt/apt.conf.d/docker-clean && \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache && \
    chmod 1777 /tmp && \
    apt update && \
    apt --no-install-recommends install -y ca-certificates curl vim unzip iproute2 fonts-dejavu fontconfig && \
    mkdir -p /usr/local/share/fonts/truetype/noto && \
    curl --retry 5 --retry-delay 2 --retry-all-errors -fsSL \
        -o /usr/local/share/fonts/truetype/noto/NotoSansCJKsc-VF.ttf \
        https://github.com/notofonts/noto-cjk/raw/main/Sans/Variable/TTF/NotoSansCJKsc-VF.ttf && \
    fc-cache -f && \
    rm -rf /var/lib/apt/lists/* && apt-get clean

# Download resource from GitHub to /usr/share/infinity
# Ship only the directories required by the runtime tokenizer:
#   rag     - base analyzer dictionaries (mandatory)
#   opencc  - Traditional/Simplified Chinese conversion
#   wordnet - WordNet resources
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends git && \
    mkdir -p /usr/share/infinity/resource && \
    if [ "$NEED_MIRROR" == "1" ]; then \
        if [ -n "$GITEE_TOKEN" ]; then \
            git clone --depth 1 --single-branch "https://oauth2:${GITEE_TOKEN}@gitee.com/infiniflow/resource" /tmp/resource; \
        else \
            git clone --depth 1 --single-branch https://github.com/infiniflow/resource.git /tmp/resource; \
        fi; \
    else \
        git clone --depth 1 --single-branch https://github.com/infiniflow/resource.git /tmp/resource; \
    fi && \
    for d in rag opencc wordnet; do \
        cp -r "/tmp/resource/$d" /usr/share/infinity/resource/; \
    done && \
    rm -rf /tmp/resource && \
    apt-get purge -y git && \
    apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/* && apt-get clean

ARG NGINX_VERSION=1.31.3-1~noble
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    apt -o Acquire::Retries=5 update && \
    apt -o Acquire::Retries=5 install -y --no-install-recommends gnupg && \
    mkdir -p /etc/apt/keyrings && \
    curl --retry 5 --retry-delay 2 --retry-all-errors -fsSL https://nginx.org/keys/nginx_signing.key | gpg --dearmor -o /etc/apt/keyrings/nginx-archive-keyring.gpg && \
    echo "deb [signed-by=/etc/apt/keyrings/nginx-archive-keyring.gpg] https://nginx.org/packages/mainline/ubuntu/ noble nginx" > /etc/apt/sources.list.d/nginx.list && \
    apt -o Acquire::Retries=5 update && \
    apt -o Acquire::Retries=5 install -y --no-install-recommends nginx=${NGINX_VERSION} && \
    apt-mark hold nginx && \
    apt-get purge -y gnupg && \
    apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/* && apt-get clean


# ── web-builder stage ──

FROM runner-${TARGETARCH} AS web-builder
USER root

WORKDIR /ragflow


# Install frontend dependencies — depends only on package manifests so
# web source / docs changes don't invalidate this layer.
COPY web/package.json web/package-lock.json web/.npmrc ./web/
RUN --mount=type=cache,id=ragflow_npm,target=/root/.npm,sharing=locked \
    cd web && NODE_OPTIONS="--max-old-space-size=8192" npm install

# Copy full web source and docs for the frontend build.
COPY web web
COPY docs docs
RUN --mount=type=cache,id=ragflow_npm,target=/root/.npm,sharing=locked \
    cd web && NODE_OPTIONS="--max-old-space-size=8192" VITE_BUILD_SOURCEMAP=false VITE_MINIFY=esbuild npm run build

# Stamp the build version into /ragflow/VERSION. Requires git, which must be
# preinstalled in the github_action_runner base image (the former apt-get install
# git step was removed), and the host .git tree bound in at build time.
RUN --mount=type=bind,source=.git,target=/ragflow/.git \
    version_info=$(git describe --tags --match=v* --first-parent --always) && \
    echo "$version_info" > /ragflow/VERSION



# ── go-builder stage ──
FROM runner-${TARGETARCH} AS go-builder
USER root
SHELL ["/bin/bash", "-c"]
WORKDIR /ragflow

# Pin GOMODCACHE/GOCACHE to the BuildKit cache-mount paths for the whole stage.
# Without this, the `GOMODCACHE=... GOCACHE=...` prefix on the RUN lines below only
# applies to the command before `&&`, so `./build.sh --go` (the command after `&&`)
# falls back to the default /root/go/pkg/mod and re-downloads every module that
# `go mod download` already fetched. Exporting them as ENV makes every go command in
# this stage — including the `go build` inside build.sh — share the same persistent
# cache mount, so the download happens once and is reused across builds (the CI
# runner shares the host Docker daemon, so the mount persists across jobs/replicas).
ENV GOMODCACHE=/root/.cache/gomod GOCACHE=/root/.cache/gobuild

# GOPROXY is overridable via --build-arg. CI passes a proxy that is reliable
# from GitHub runners (e.g. proxy.golang.org); the default keeps goproxy.cn
# first for developers behind the GFW. Both `go mod download` below and the
# `go build` inside build.sh inherit this ENV.
ARG GOPROXY
ENV GOPROXY="${GOPROXY:-https://goproxy.cn,https://proxy.golang.org,direct}"

# Cache Go modules BEFORE copying source (mirrors the Dockerfile_ci fix):
# copy only the manifests, download the full module graph into a persistent
# BuildKit cache mount, then bring in source. GOMODCACHE/GOCACHE are pinned to the
# mounted paths so `go mod download` and `build.sh --go` share the same cache and
# dependencies are never re-fetched when only source changes.
COPY go.mod go.sum ./
# Retry the module download: Go module proxies occasionally drop the HTTP/2
# connection (GOAWAY) mid-transfer, which is transient and recovers on retry.
# Exit nonzero if all attempts fail, so the build fails fast instead of
# continuing with an incomplete module cache (which would surface later in
# build.sh as a confusing missing-dependency error).
RUN --mount=type=cache,id=ragflow_gomod,target=/root/.cache/gomod,sharing=locked \
    --mount=type=cache,id=ragflow_gobuild,target=/root/.cache/gobuild \
    ok=0; \
    for i in 1 2 3 4 5; do \
      if go mod download; then ok=1; break; fi; \
      echo "go mod download attempt $i failed, retrying in 5s" >&2; \
      [ "$i" -lt 5 ] && sleep 5; \
    done; \
    if [ "$ok" -ne 1 ]; then echo "go mod download failed after 5 attempts" >&2; exit 1; fi

# Fetch and unpack native static libraries (office_oxide, pdfium, pdf_oxide,
# onnxruntime) into ${HOME}/ragflow-native-libs before copying source so code
# edits keep this layer cached and upstream runner image drift never breaks builds.
ARG ORT_VERSION=1.29.0
ARG ORT_RELEASE_TAG=onnxruntime-v1.29.0-patch2
ARG PDFIUM_STATIC_VERSION=7809
ARG PDF_OXIDE_VERSION=0.3.73
RUN --mount=type=cache,id=ragflow_native_archives,target=/tmp/native_archives,sharing=locked \
    set -euo pipefail; \
    arch="$(uname -m)"; \
    case "$arch" in \
      x86_64|amd64) oo_asset="native-linux-x86_64.tar.gz"; pdfium_asset="pdfium-linux-x64-static.tgz"; pdf_oxide_asset="pdf_oxide-go-ffi-linux-amd64.tar.gz"; ort_target="linux-x86_64"; ort_arch="x64" ;; \
      aarch64|arm64) oo_asset="native-linux-aarch64.tar.gz"; pdfium_asset="pdfium-linux-arm64-static.tgz"; pdf_oxide_asset="pdf_oxide-go-ffi-linux-arm64.tar.gz"; ort_target="linux-aarch64"; ort_arch="aarch64" ;; \
      *) echo "Unsupported arch: $arch" >&2; exit 1 ;; \
    esac; \
    oo_ver="$(grep -m1 -E '^[[:space:]]*github.com/yfedoseev/office_oxide/go[[:space:]]+v' go.mod | sed -E 's/.*v([0-9][^[:space:]]*).*/\1/')"; \
    mkdir -p "${HOME}/ragflow-native-libs/office_oxide" "${HOME}/ragflow-native-libs/pdfium-static" "${HOME}/ragflow-native-libs/pdf_oxide" "${HOME}/ragflow-native-libs/onnxruntime/static_lib"; \
    fetch_if_missing() { \
      local url="$1" dest="$2"; \
      if [ ! -s "$dest" ]; then \
        curl --retry 5 --retry-delay 2 --retry-all-errors -fsSL -o "${dest}.tmp" "$url" && mv "${dest}.tmp" "$dest"; \
      fi; \
    }; \
    fetch_if_missing "https://github.com/yfedoseev/office_oxide/releases/download/v${oo_ver}/${oo_asset}" "/tmp/native_archives/${oo_asset}"; \
    tar -xzf "/tmp/native_archives/${oo_asset}" -C "${HOME}/ragflow-native-libs/office_oxide"; \
    fetch_if_missing "https://github.com/kognitos/pdfium-static/releases/download/chromium%2F${PDFIUM_STATIC_VERSION}/${pdfium_asset}" "/tmp/native_archives/${pdfium_asset}"; \
    tar -xzf "/tmp/native_archives/${pdfium_asset}" -C "${HOME}/ragflow-native-libs/pdfium-static"; \
    fetch_if_missing "https://github.com/yfedoseev/pdf_oxide/releases/download/v${PDF_OXIDE_VERSION}/${pdf_oxide_asset}" "/tmp/native_archives/${pdf_oxide_asset}"; \
    tar -xzf "/tmp/native_archives/${pdf_oxide_asset}" -C "${HOME}/ragflow-native-libs/pdf_oxide"; \
    ort_zip="onnxruntime-v${ORT_VERSION}-${ort_target}.zip"; \
    fetch_if_missing "https://github.com/infiniflow/ragflow-build/releases/download/${ORT_RELEASE_TAG}/${ort_zip}" "/tmp/native_archives/${ort_zip}"; \
    stage_dir="$(mktemp -d)"; \
    unzip -q "/tmp/native_archives/${ort_zip}" -d "$stage_dir"; \
    extracted_dir="$(find "$stage_dir" -mindepth 1 -maxdepth 1 -type d | head -1)"; \
    mv "$extracted_dir" "${HOME}/ragflow-native-libs/onnxruntime/static_lib/onnxruntime-linux-${ort_arch}-static_lib-${ORT_VERSION}-glibc2_28"; \
    rm -rf "$stage_dir"

COPY internal internal
COPY cmd cmd
COPY build.sh ./
COPY Dockerfile ./

RUN git config --global safe.directory "*" && \
    cd /ragflow && ./build.sh --cpp

RUN --mount=type=cache,id=ragflow_gomod,target=/root/.cache/gomod,sharing=locked \
    --mount=type=cache,id=ragflow_gobuild,target=/root/.cache/gobuild \
    GOMODCACHE=/root/.cache/gomod GOCACHE=/root/.cache/gobuild \
    git config --global safe.directory "*" && \
    ./build.sh --go


##### production stage
FROM base AS production
USER root

WORKDIR /ragflow

# Copy the compiled Go backend binaries (set exec bits at copy time to avoid a redundant chmod layer)
COPY --from=go-builder --chmod=755 /ragflow/bin/ragflow_server /ragflow/bin/ragflow_server

ENV PYTHONPATH=/ragflow/

COPY docker/service_conf.yaml.template ./conf/service_conf.yaml.template
COPY --chmod=755 docker/entrypoint*.sh ./

# Copy nginx configuration for frontend serving
RUN mkdir -p /etc/nginx/conf.d /var/log/nginx

COPY docker/nginx/nginx.conf docker/nginx/proxy.conf /etc/nginx/
COPY docker/nginx/ragflow.conf /etc/nginx/conf.d/

RUN rm -f /etc/nginx/sites-enabled/default


COPY conf conf
COPY internal/agent/templates agent/templates

# Wiki page-structure presets read at runtime by the Go backend
# (CompilationTemplateService.LoadWikiPresets).
COPY internal/ingestion/knowledge_compile/templates ./internal/ingestion/knowledge_compile/templates


# Copy compiled web pages
COPY --from=web-builder /ragflow/web/dist /ragflow/web/dist

# Copy version info
COPY --from=web-builder /ragflow/VERSION /ragflow/VERSION

# Set environment variables
ENV HF_ENDPOINT=https://hf-mirror.com

ENTRYPOINT ["./entrypoint.sh"]
