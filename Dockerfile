# syntax=docker/dockerfile:1

# ==============================================================================
# Agent Flow 容器映像：內含 agent-flow、CAO (cao / cao-server)、tmux 與各 Provider CLI
# ==============================================================================

FROM golang:1.24-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /agent-flow ./cmd/agent-flow

FROM node:24-bookworm-slim

ARG CAO_VERSION=2.3.0
ARG CLAUDE_CODE_VERSION=2.1.291
ARG CODEX_VERSION=0.160.1
ARG GLAB_VERSION=1.36.0
ARG TARGETARCH

# 容器使用者需與主機使用者一致 (UID/GID/HOME)，
# 讓掛載進來的專案、憑證與 Provider 設定路徑和主機完全相同
ARG USER_UID=1000
ARG USER_GID=1000
ARG USER_HOME=/home/agent

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git openssh-client tini tmux \
    && rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH:-amd64}" in \
        amd64) GLAB_ARCH=x86_64 ;; \
        arm64) GLAB_ARCH=arm64 ;; \
        *) echo "不支援的架構: ${TARGETARCH}" && exit 1 ;; \
    esac \
    && curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads/glab_${GLAB_VERSION}_Linux_${GLAB_ARCH}.tar.gz" \
        | tar -xz -C /usr/local bin/glab

RUN npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "@openai/codex@${CODEX_VERSION}" \
    && npm cache clean --force

COPY --from=ghcr.io/astral-sh/uv:0.11.30 /uv /usr/local/bin/uv
ENV UV_TOOL_DIR=/opt/uv/tools \
    UV_TOOL_BIN_DIR=/usr/local/bin \
    UV_PYTHON_INSTALL_DIR=/opt/uv/python
RUN uv tool install --python 3.12 "cli-agent-orchestrator==${CAO_VERSION}" \
    && chmod -R a+rX /opt/uv

# node 映像內建 UID/GID 1000 的 node 使用者，依主機設定調整 UID/GID 與家目錄；
# 並預先建立掛載點的上層目錄，避免 Docker 以 root 身分自動建立導致 CAO 無法寫入
RUN groupmod -g "${USER_GID}" node \
    && usermod -u "${USER_UID}" -g "${USER_GID}" -d "${USER_HOME}" -m node \
    && mkdir -p "${USER_HOME}/.aws/cli-agent-orchestrator" "${USER_HOME}/.config" \
    && chown -R "${USER_UID}:${USER_GID}" "${USER_HOME}"

COPY --from=build /agent-flow /usr/local/bin/agent-flow
COPY scripts/bootstrap-cao.sh scripts/docker-entrypoint.sh /usr/local/lib/agent-flow/

USER node
ENV HOME=${USER_HOME} \
    TERM=xterm-256color

ENTRYPOINT ["tini", "--", "/usr/local/lib/agent-flow/docker-entrypoint.sh"]
