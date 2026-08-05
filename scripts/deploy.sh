#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Lodestar 一键部署 / 更新脚本
#
# 用法：
#   ./scripts/deploy.sh              # 拉 .env 里 LODESTAR_IMAGE_TAG 指定的版本（缺省 latest）
#   ./scripts/deploy.sh latest       # 强制拉最新 main
#   ./scripts/deploy.sh sha-e52826b  # 回滚到指定提交
#
# 前置条件：
#   - 已 cp .env.example .env 并填好密钥
#   - 服务器已装 docker 和 docker compose v2
# =============================================================================

cd "$(dirname "$0")/.." || exit 1

if [[ ! -f .env ]]; then
  echo "✗ 找不到 .env。先执行：cp .env.example .env 并填好密钥。" >&2
  exit 1
fi

# 参数指定 tag 则本次覆盖；否则从 .env 读。
#
# ★ 不要用 source/export 读 .env——实测两种坏法都会发生：
#   1) 静默截断：`LODESTAR_DATABASE_PATH=host=X port=5432 user=...` 只留下 `host=X`，
#      后面的 `port=5432` 等被当成同一行的其它变量赋值吃掉，退出码仍是 0，无人察觉。
#   2) 执行命令：若值的第二个词不是 k=v 形式（如 `SOME_NOTE=see docs/DEPLOY.md`），
#      bash 会把它当命令执行并报 127。
#   这里只按行提取需要的那一个键，不碰其余内容。
TARGET_TAG="${1:-}"
if [[ -z "$TARGET_TAG" ]]; then
  TARGET_TAG="$(sed -n 's/^[[:space:]]*LODESTAR_IMAGE_TAG[[:space:]]*=[[:space:]]*//p' .env | tail -n1 | tr -d '"'\''[:space:]')"
fi
TARGET_TAG="${TARGET_TAG:-latest}"

# compose 通过 env_file 读 .env，但 image: 里的 ${LODESTAR_IMAGE_TAG} 是在
# 解析 compose 文件时插值的，走的是 shell 环境（不是 env_file），故必须显式 export。
export LODESTAR_IMAGE_TAG="$TARGET_TAG"
IMAGE="ghcr.io/gypg/lodestar:${TARGET_TAG}"

echo "==> 目标镜像：$IMAGE"

OLD_DIGEST="$(docker inspect --format '{{.Image}}' lodestar 2>/dev/null || true)"

echo "==> 拉取镜像"
docker compose pull

echo "==> 启动 / 滚动升级容器"
docker compose up -d

NEW_DIGEST="$(docker inspect --format '{{.Image}}' lodestar 2>/dev/null || true)"
if [[ -n "$OLD_DIGEST" && "$OLD_DIGEST" == "$NEW_DIGEST" ]]; then
  echo "   （镜像未变化，容器沿用现有版本）"
fi

# 等健康检查。compose 里 start_period=10s、interval=30s，
# 故首次 healthy 最早也要 ~10s，最坏要一个 interval，这里给 90s 上限。
echo "==> 等待健康检查（最多 90s）"
DEADLINE=$((SECONDS + 90))
while ((SECONDS < DEADLINE)); do
  STATUS="$(docker inspect --format '{{.State.Health.Status}}' lodestar 2>/dev/null || echo "missing")"
  case "$STATUS" in
    healthy)
      echo "✓ 部署成功，容器健康。运行版本：$IMAGE"
      docker compose logs --tail=5 lodestar || true
      echo
      echo "  日志：docker compose logs -f lodestar"
      exit 0
      ;;
    unhealthy)
      echo "✗ 健康检查失败（unhealthy）。最近日志：" >&2
      docker compose logs --tail=40 lodestar >&2 || true
      echo >&2
      echo "  回滚：./scripts/deploy.sh sha-<上一个可用commit>" >&2
      exit 1
      ;;
    missing)
      echo "✗ 容器 lodestar 不存在，启动失败。最近日志：" >&2
      docker compose logs --tail=40 lodestar >&2 || true
      exit 1
      ;;
  esac
  sleep 3
done

echo "✗ 90s 内未变 healthy（当前状态：${STATUS}）。最近日志：" >&2
docker compose logs --tail=40 lodestar >&2 || true
exit 1
