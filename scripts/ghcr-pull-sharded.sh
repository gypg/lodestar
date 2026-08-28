#!/bin/bash
#
# GHCR 分片拉取 —— 当 `docker pull` 两条链路都卡死时的绕过方案。
#
# 为什么需要它：在这台服务器上 GHCR 会间歇性地"manifest 秒回、blob 一个字节都不动"，
# NJU 镜像站也时常拉不动。poller 的 deploy.sh 遇到这种情况会保留旧镜像继续跑
# （日志里那句"registry 上 latest 与本地一致"是失败回落的提示，不是比对结论），
# 于是生产停在旧版本。已实测两次（2026-08-17、2026-08-27）。
#
# 为什么分片有效：单连接 5-10KB/s 且持续衰减，但**多连接并发总速 90-110KB/s**
# —— 限速是按连接算的，不是按 IP。GHCR 的 blob 支持 Range 请求（HTTP 206）。
#
# 做法：8 路并发 Range 下载每个 blob（带断点续传与 SHA256 校验），拼成 OCI layout
# 目录，再 `docker load`。Docker 29.6 能直接吃 OCI layout tar；旧版 docker load 期望
# docker save 格式，会报 "cannot unmarshal object"。
#
# 用法：  bash ghcr-pull-sharded.sh [owner/repo] [tag]
# 默认：  gypg/lodestar latest
#   （用 bash 显式调用：本仓库脚本在 git index 里是 100644，新克隆上没有 exec 位。）
#
# 完成后本地就有该 tag，接着跑 `bash deploy.sh` 完成滚动升级（它内部是
# `up -d --pull never`）。★ 不要直接 `docker compose up -d`：compose 里
# pull_policy: always，它会重新去 pull，正是本脚本要绕开的那一步。
set -uo pipefail

REPO="${1:-gypg/lodestar}"
TAG="${2:-latest}"
REGISTRY="ghcr.io"
SHARDS="${SHARDS:-8}"
# 工作目录固定不带 $$：blob 下载是分钟级的，中途失败后重跑要能复用已校验的 blob
# （fetch_blob 开头会校验并跳过）。带 PID 的话每次重跑都是空目录，断点续传形同虚设。
WORK="${WORK:-/tmp/ghcr-sharded-${REPO//\//-}-${TAG}}"

log() { printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
die() { log "✗ $*"; exit 1; }

command -v python3 >/dev/null || die "需要 python3 解析 manifest"

REPO_ENC="${REPO//\//%2F}"
log "取匿名 token: $REPO"
TOKEN=$(curl -s -m 30 "https://${REGISTRY}/token?scope=repository%3A${REPO_ENC}%3Apull" |
	python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))')
[ -n "$TOKEN" ] || die "拿不到 token"

AUTH=(-H "Authorization: Bearer $TOKEN")
ACCEPT_LIST=(-H "Accept: application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json")

log "取 $TAG 的 manifest"
INDEX=$(curl -s -m 30 "${AUTH[@]}" "${ACCEPT_LIST[@]}" \
	"https://${REGISTRY}/v2/${REPO}/manifests/${TAG}")
[ -n "$INDEX" ] || die "manifest 为空"

# 多架构 index 时挑 linux/amd64；单架构 manifest 则直接用 tag 本身。
AMD_DIGEST=$(printf '%s' "$INDEX" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if d.get("manifests"):
    for m in d["manifests"]:
        p=m.get("platform",{})
        if p.get("architecture")=="amd64" and p.get("os")=="linux":
            print(m["digest"]); break
')
MANIFEST_REF="${AMD_DIGEST:-$TAG}"
log "amd64 manifest: $MANIFEST_REF"

MANIFEST=$(curl -s -m 30 "${AUTH[@]}" "${ACCEPT_LIST[@]}" \
	"https://${REGISTRY}/v2/${REPO}/manifests/${MANIFEST_REF}")
[ -n "$MANIFEST" ] || die "取不到 amd64 manifest"

# manifest 自身的 digest 要用**原始字节**算，不能用重新序列化的 JSON ——
# 空格或键序差一点，digest 就不是 registry 认的那个，docker load 会拒绝。
MANIFEST_DIGEST="sha256:$(printf '%s' "$MANIFEST" | sha256sum | cut -d' ' -f1)"
if [ -n "$AMD_DIGEST" ] && [ "$MANIFEST_DIGEST" != "$AMD_DIGEST" ]; then
	log "⚠ 本地算出的 manifest digest ($MANIFEST_DIGEST) 与 index 里的 ($AMD_DIGEST) 不一致"
	log "  用 index 里的那个作为权威值（curl 可能改写了响应体的空白）"
	MANIFEST_DIGEST="$AMD_DIGEST"
fi

mkdir -p "$WORK/blobs/sha256" || die "建工作目录失败"
# 只在成功后清理：失败时保留已下完并校验过的 blob，供下次重跑复用。
# 每个 blob 都经过 SHA256 校验才留下，所以复用是安全的；校验不过的会被删掉重下。
cleanup_on_success() { rm -rf "$WORK"; }

# blob 清单：config + 各 layer。
mapfile -t BLOBS < <(printf '%s' "$MANIFEST" | python3 -c '
import json,sys
m=json.load(sys.stdin)
print(m["config"]["digest"], m["config"]["size"])
for l in m.get("layers",[]):
    print(l["digest"], l["size"])
')
[ "${#BLOBS[@]}" -gt 0 ] || die "manifest 里没有 blob"

# config digest 单独留一份：它就是 docker 的**镜像 ID**，后面打 tag 要用它。
# 不能用 manifest digest —— `docker tag` 只认镜像 ID 或已有的镜像名，
# 传 manifest digest 会报 "No such image"。
CONFIG_DIGEST="${BLOBS[0]%% *}"

# fetch_blob 下载一个 blob 到 blobs/sha256/<hex>，已存在且校验通过则跳过。
#
# 每个分片单独重试而不是整条重下：连接会在传输中途死掉，整条重下等于永远从 0 开始，
# 这正是 docker pull 卡死的表现。分片 + 断点续传把已下到的字节留住。
fetch_blob() {
	local digest="$1" size="$2"
	local hex="${digest#sha256:}"
	local out="$WORK/blobs/sha256/$hex"
	local url="https://${REGISTRY}/v2/${REPO}/blobs/${digest}"

	if [ -f "$out" ] && [ "$(sha256sum "$out" | cut -d' ' -f1)" = "$hex" ]; then
		log "  已有且校验通过，跳过 ${hex:0:12}"
		return 0
	fi

	local chunk=$(((size + SHARDS - 1) / SHARDS))
	log "  下载 ${hex:0:12}（$size 字节，$SHARDS 路，每路约 $chunk 字节）"

	local i pids=()
	for ((i = 0; i < SHARDS; i++)); do
		local start=$((i * chunk))
		local end=$((start + chunk - 1))
		[ "$end" -ge "$size" ] && end=$((size - 1))
		[ "$start" -gt "$end" ] && continue
		(
			local part="$out.part$i"
			local attempt=0
			while :; do
				local have=0
				[ -f "$part" ] && have=$(stat -c %s "$part" 2>/dev/null || echo 0)
				local want=$((end - start + 1))
				[ "$have" -ge "$want" ] && break
				attempt=$((attempt + 1))
				if [ "$attempt" -gt 40 ]; then
					echo "shard $i 重试 40 次仍未完成" >&2
					exit 1
				fi
				# -L 是必需的：GHCR 的 blob 端点返回 **307**，重定向到
				# pkg-containers.githubusercontent.com 上一个带签名的短期 URL。
				# 不跟重定向的话拿到的是零字节，8 路分片会全部"重试到上限"，
				# 看起来像网络慢，其实一个字节都没请求到（实测踩过）。
				#
				# 从已下到的位置继续，避免重下。--max-time 限制单次尝试，
				# 卡死的连接会被切断而不是一直挂着。
				curl -sL --max-time 100 "${AUTH[@]}" \
					-H "Range: bytes=$((start + have))-${end}" \
					"$url" >>"$part" 2>/dev/null || true
			done
		) &
		pids+=($!)
	done

	local rc=0
	for p in "${pids[@]}"; do wait "$p" || rc=1; done
	[ "$rc" -eq 0 ] || { log "  分片下载失败：${hex:0:12}"; return 1; }

	cat "$out".part* >"$out" && rm -f "$out".part*
	local got
	got=$(sha256sum "$out" | cut -d' ' -f1)
	if [ "$got" != "$hex" ]; then
		log "  SHA256 不符：want ${hex:0:16} got ${got:0:16}"
		rm -f "$out"
		return 1
	fi
	log "  ✓ ${hex:0:12} 校验通过"
}

for entry in "${BLOBS[@]}"; do
	set -- $entry
	fetch_blob "$1" "$2" || die "blob $1 下载失败"
done

# manifest 自己也要作为 blob 落盘（index.json 指向它）。
printf '%s' "$MANIFEST" >"$WORK/blobs/sha256/${MANIFEST_DIGEST#sha256:}"

MANIFEST_SIZE=$(stat -c %s "$WORK/blobs/sha256/${MANIFEST_DIGEST#sha256:}")
MEDIA_TYPE=$(printf '%s' "$MANIFEST" | python3 -c '
import json,sys
print(json.load(sys.stdin).get("mediaType","application/vnd.oci.image.manifest.v1+json"))
')

printf '{"imageLayoutVersion":"1.0.0"}' >"$WORK/oci-layout"
cat >"$WORK/index.json" <<JSON
{"schemaVersion":2,"manifests":[{"mediaType":"${MEDIA_TYPE}","digest":"${MANIFEST_DIGEST}","size":${MANIFEST_SIZE},"annotations":{"io.containerd.image.name":"${REGISTRY}/${REPO}:${TAG}","org.opencontainers.image.ref.name":"${TAG}"}}]}
JSON

log "docker load（OCI layout）"
tar -cf - -C "$WORK" oci-layout index.json blobs | docker load || die "docker load 失败"

# 打上目标 tag。index.json 里的 io.containerd.image.name 通常已让 docker load 直接
# 建好 tag，这个分支是兜底。
#
# ★ 这里必须传 CONFIG_DIGEST（= 镜像 ID），不能传 MANIFEST_DIGEST：
#   `docker tag` 只接受镜像 ID 或已存在的镜像名，manifest digest 两者都不是。
if ! docker image inspect "${REGISTRY}/${REPO}:${TAG}" >/dev/null 2>&1; then
	docker tag "$CONFIG_DIGEST" "${REGISTRY}/${REPO}:${TAG}" ||
		die "打 tag 失败（尝试 docker tag $CONFIG_DIGEST ${REGISTRY}/${REPO}:${TAG}）"
fi

# ★ 本地 RepoDigests **不会**等于 registry 上 latest 的 list digest —— 实测
#   repoDigest=...567b4d9c 是 amd64 manifest 的 digest，而 poller 用
#   `Accept: ...index.v1+json` 查到的是 list digest ...7ab0c77，两者天然不等。
#   （早先注释里"docker tag 后两者会相等"的说法是错的，已按实测更正。）
#
#   后果不是坏事，但要知道会发生什么：poller 下一轮比对会判定"有新版本"并调
#   deploy.sh。此时所有 layer 已在本地，`docker compose pull` 只取 manifest+config，
#   秒级完成，顺带把 RepoDigests 写回 list digest，于是**下一轮**才真正安静。
#   即分片拉取之后会多跑一次几乎零成本的 pull，不是重新下载整个镜像。

log "✓ 完成。本地镜像："
# ★ revision label 未必等于运行时代码：曾出现 :latest 的 revision 指向一个 docs-only
#   commit(20b1b86) 而镜像内代码实为 f1483ab。核实生产版本要用 /api/status 一类
#   运行时端点，不要只信这个 label。
docker image inspect "${REGISTRY}/${REPO}:${TAG}" \
	--format '  revision={{index .Config.Labels "org.opencontainers.image.revision"}}
  imageID={{.Id}}
  repoDigest={{if .RepoDigests}}{{index .RepoDigests 0}}{{else}}<none>{{end}}'
cleanup_on_success
log "接下来：跑 ./deploy.sh 完成滚动升级（内部 up -d --pull never）。"
log "不要直接 docker compose up -d —— pull_policy: always 会再去拉一次。"
