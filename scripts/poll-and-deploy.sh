#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Lodestar 自动部署轮询器
#
# 定期查询 GHCR registry 上 ghcr.io/gypg/lodestar:latest 的 manifest digest，
# 与本地 tag 对比，发现变化则调用 deploy.sh 完成拉取+升级。
#
# 设计：
#   - 只做 digest 对比，不拉镜像（HEAD 请求 <1KB）
#   - 超时保护（GHCR 国内间歇性卡死）
#   - 复用 deploy.sh 的完整逻辑（超时+回落+健康检查+失败回滚）
#   - 无状态：失败静默（日志可见），下轮自然重试
#
# 用法（cron）：
#   */10 * * * * flock -n /tmp/lodestar-poll.lock /opt/docker/lodestar/scripts/poll-and-deploy.sh >> /var/log/lodestar-poll.log 2>&1
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT" || exit 1

IMAGE_REPO="ghcr.io/gypg/lodestar"
IMAGE_TAG="latest"
IMAGE_REF="${IMAGE_REPO}:${IMAGE_TAG}"

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

# 1. 查询 registry 上 latest 的 digest（匿名认证，HEAD 请求）
#
# GHCR 需要先获取匿名 token，再带 token 查 manifest。
# 超时保护：GHCR 国内直连可能卡死，所以两步都加超时。
log "查询 registry digest: ${IMAGE_REF}"

TOKEN=$(curl -sf --max-time 15 \
  "https://ghcr.io/token?scope=repository%3Agypg%2Flodestar%3Apull&service=ghcr.io" \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p' \
  2>/dev/null || true)

if [[ -z "$TOKEN" ]]; then
  log "✗ 无法获取 registry token（超时或网络问题），跳过本轮"
  exit 0
fi

# Accept 多种 manifest 格式，GHCR 返回 docker-content-digest header
ACCEPT_HEADER="Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json"

REMOTE_DIGEST=$(curl -sfI --max-time 20 \
  -H "Authorization: Bearer $TOKEN" \
  -H "$ACCEPT_HEADER" \
  "https://ghcr.io/v2/gypg/lodestar/manifests/${IMAGE_TAG}" \
  | sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: *//p' \
  | tr -d '\r' \
  2>/dev/null || true)

if [[ -z "$REMOTE_DIGEST" ]]; then
  log "✗ 无法获取 registry manifest digest（超时或 API 变化），跳过本轮"
  exit 0
fi

log "  registry digest: ${REMOTE_DIGEST}"

# 2. 读本地 latest tag 的 digest
#
# docker image inspect 返回 RepoDigests 数组，格式 "ghcr.io/gypg/lodestar@sha256:..."
# 提取 sha256:... 部分与 registry 的比对。
LOCAL_REF=$(docker image inspect "$IMAGE_REF" --format '{{index .RepoDigests 0}}' 2>/dev/null || true)

if [[ -z "$LOCAL_REF" ]]; then
  log "  本地无此镜像，视为需要部署"
  LOCAL_DIGEST=""
else
  LOCAL_DIGEST="${LOCAL_REF##*@}"  # 提取 @ 后面的 sha256:...
  log "  本地 digest:     ${LOCAL_DIGEST}"
fi

# 3. 对比 digest，不同则触发部署
if [[ "$REMOTE_DIGEST" == "$LOCAL_DIGEST" ]]; then
  log "✓ 已是最新版本，无需部署"
  exit 0
fi

log "==> 检测到新版本，开始部署"
log "    旧: ${LOCAL_DIGEST:-<本地无>}"
log "    新: ${REMOTE_DIGEST}"

# 调用 deploy.sh（已有完整的拉取+超时+回落+健康检查+失败回滚）
if bash "${SCRIPT_DIR}/deploy.sh" "${IMAGE_TAG}"; then
  log "✓ 部署成功"
else
  log "✗ 部署失败（退出码 $?），详见上方 deploy.sh 输出"
  exit 1
fi
