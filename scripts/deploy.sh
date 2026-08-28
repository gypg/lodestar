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

# 确保 data/ 存在且可被容器内的非 root 进程写入。
#
# ★ 镜像以 UID/GID 1000(lodestar) 运行，data/ 是 bind mount——宿主的 owner 直接
#   决定容器内能否写。若用 root 手工 mkdir，容器启动时会死在：
#     failed to create default config "/app/data/config.json": permission denied
#   而这个坑只在全新部署时出现：已有部署的 data/ 早就是 1000:1000，升级时看不到，
#   所以极易漏掉——本项目就是重建服务器时才暴露的。
mkdir -p data
CURRENT_OWNER="$(stat -c '%u:%g' data)"
if [[ "$CURRENT_OWNER" != "1000:1000" ]]; then
  echo "==> data/ 归属为 ${CURRENT_OWNER}，容器需要 1000:1000，正在修正"
  if ! chown -R 1000:1000 data 2>/dev/null; then
    echo "✗ 无法 chown data/（当前用户非 root？）。请手动执行：" >&2
    echo "    sudo chown -R 1000:1000 $(pwd)/data" >&2
    exit 1
  fi
fi

# compose 通过 env_file 读 .env，但 image: 里的 ${LODESTAR_IMAGE_TAG} 是在
# 解析 compose 文件时插值的，走的是 shell 环境（不是 env_file），故必须显式 export。
export LODESTAR_IMAGE_TAG="$TARGET_TAG"
IMAGE="ghcr.io/gypg/lodestar:${TARGET_TAG}"

echo "==> 目标镜像：$IMAGE"

# 记录 pull 前 tag 指向的镜像 ID。
# 判断「有没有真的取回新镜像」要比对 tag 的前后指向，而不是容器的镜像：
# 上一次部署若在 up 阶段失败（如端口冲突），容器会停在 Created 且已指向新镜像，
# 此时容器前后 digest 相同，会把真实的升级误报成「镜像未变化」。
PRE_PULL_DIGEST="$(docker image inspect --format '{{.Id}}' "$IMAGE" 2>/dev/null || true)"

# 拉取镜像。
#
# ★ 国内服务器实测：直连 ghcr.io 拉 blob 会无限卡在 "Pulling fs layer"——
#   manifest 元数据秒回（HTTP 401/200 正常），但层数据一个字节都不动，
#   docker compose pull 没有内建超时，会一直挂着。所以这里必须自己加 timeout，
#   并在超时后回落到 NJU 镜像站拉取 + retag 回 ghcr.io 名字（compose 认的是这个名字）。
#
#   注意别用 `docker pull ... | tail` 判断成败：管道会把退出码换成 tail 的，
#   卡死的 pull 也会显示 exit=0。必须直接取 docker 自己的退出码。
MIRROR="ghcr.nju.edu.cn/gypg/lodestar"
PULL_TIMEOUT="${LODESTAR_PULL_TIMEOUT:-120}"

echo "==> 拉取镜像（直连 ghcr.io，最多 ${PULL_TIMEOUT}s）"
if timeout "$PULL_TIMEOUT" docker compose pull; then
  echo "   直连拉取成功"
else
  echo "   直连超时/失败，回落到镜像站 $MIRROR"
  if timeout "$PULL_TIMEOUT" docker pull "${MIRROR}:${TARGET_TAG}"; then
    docker tag "${MIRROR}:${TARGET_TAG}" "$IMAGE"
    echo "   已从镜像站拉取并 retag 为 $IMAGE"
  else
    # 用 bash 显式调用：仓库里这些脚本在 index 里是 100644（Windows 检出丢 exec 位），
    # 直接 ./xxx.sh 在新克隆上会 Permission denied。
    echo "✗ 镜像站也拉不动。用分片拉取绕过（不要反复 docker pull —— 它会卡在 blob 上占住连接）：" >&2
    echo "    bash $(dirname "$0")/ghcr-pull-sharded.sh && bash $(dirname "$0")/deploy.sh ${TARGET_TAG}" >&2
    echo "  它 8 路并发 Range 下载各 blob 再 docker load。单连接被限速到 5-10KB/s 并持续衰减，" >&2
    echo "  但限速是按连接算的，多路并发能到 90-110KB/s；两个 28MB 大层约 10 分钟。" >&2
    docker image inspect "$IMAGE" >/dev/null 2>&1 || exit 1
    echo "   本地已存在 $IMAGE，用它继续 —— 注意这可能是**旧**镜像，本轮升级实际未生效。" >&2
  fi
fi

# 回落路径已把镜像准备好，此处不能再让 compose 去 pull（pull_policy: always 会重蹈覆辙）。
echo "==> 启动 / 滚动升级容器"
docker compose up -d --pull never

# 读 pull 后 tag 指向的镜像 ID，对比是否真的取回了新版。
POST_PULL_DIGEST="$(docker image inspect --format '{{.Id}}' "$IMAGE" 2>/dev/null || true)"

# 「有没有升级」要看 tag 指向的镜像在 pull 前后变没变，不能看容器的镜像：
# 上一次部署若在 up 阶段失败（如端口冲突），容器会停在 Created 且已指向新镜像，
# 此时容器前后 digest 相同，会把真实的升级误报成「镜像未变化」。
if [[ -n "$PRE_PULL_DIGEST" && "$PRE_PULL_DIGEST" == "$POST_PULL_DIGEST" ]]; then
  echo "   （registry 上 $TARGET_TAG 与本地一致，未取回新镜像）"
else
  echo "   镜像已更新：${PRE_PULL_DIGEST:-<本地无>} -> ${POST_PULL_DIGEST}"
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
