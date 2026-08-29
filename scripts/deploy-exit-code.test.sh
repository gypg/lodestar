#!/usr/bin/env bash
# =============================================================================
# deploy.sh 退出码守卫
#
# 钉住的缺陷：拉取两条链路都失败时，deploy.sh 会回落到本地已有镜像继续启动，
# 容器（跑着旧代码）健康，于是 exit 0 —— 调用方 poll-and-deploy.sh 据此打出
# 「✓ 部署成功」。2026-08-28 一天内发生四次，每次都靠人工核 revision label 才发现。
#
# 做法：把 docker / docker compose 换成桩，喂出各种场景，只断言退出码。
# 不碰真 docker，不碰真容器。
#
# 用法：bash scripts/deploy-exit-code.test.sh
# =============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY="${SCRIPT_DIR}/deploy.sh"
PASS=0
FAIL=0

# run_case <名称> <期望退出码> <pull是否成功> <容器revision> <镜像revision>
run_case() {
  local name="$1" want="$2" pull_ok="$3" running_rev="$4" image_rev="$5"
  local work
  work="$(mktemp -d)"

  # deploy.sh 会 cd 到脚本父目录，所以桩要放在 work/scripts/ 下并在 work/ 建 .env
  mkdir -p "$work/scripts" "$work/data" "$work/bin"
  cp "$DEPLOY" "$work/scripts/deploy.sh"
  printf 'LODESTAR_IMAGE_TAG=latest\n' > "$work/.env"

  # docker 桩：inspect 按被问对象返回不同 revision；pull 按场景成败
  cat > "$work/bin/docker" <<STUB
#!/usr/bin/env bash
case "\$1 \$2" in
  "compose pull")   exit $([ "$pull_ok" = "yes" ] && echo 0 || echo 1) ;;
  "compose up")     exit 0 ;;
  "compose logs")   exit 0 ;;
esac
case "\$1" in
  pull) exit $([ "$pull_ok" = "yes" ] && echo 0 || echo 1) ;;
  tag)  exit 0 ;;
  inspect)
    # 容器名 lodestar -> 容器信息；否则视为镜像
    if printf '%s\n' "\$@" | grep -q '^lodestar\$'; then
      if printf '%s\n' "\$@" | grep -q 'State.Health.Status'; then echo healthy; exit 0; fi
      echo "$running_rev"; exit 0
    fi
    if printf '%s\n' "\$@" | grep -q 'State.Health.Status'; then echo healthy; exit 0; fi
    if printf '%s\n' "\$@" | grep -q '.Id'; then echo "sha256:stub-id"; exit 0; fi
    echo "$image_rev"; exit 0 ;;
  image)
    if printf '%s\n' "\$@" | grep -q '.Id'; then echo "sha256:stub-id"; exit 0; fi
    if printf '%s\n' "\$@" | grep -q 'RepoDigests'; then echo "img@sha256:stub"; exit 0; fi
    echo "$image_rev"; exit 0 ;;
esac
exit 0
STUB
  chmod +x "$work/bin/docker"

  # stat 桩：让 data/ 归属检查直接通过，避免脚本尝试 chown
  cat > "$work/bin/stat" <<'STUB'
#!/usr/bin/env bash
echo "1000:1000"
STUB
  chmod +x "$work/bin/stat"

  local out rc
  out="$(cd "$work" && PATH="$work/bin:$PATH" LODESTAR_PULL_TIMEOUT=1 \
        bash "$work/scripts/deploy.sh" latest 2>&1)"
  rc=$?

  if [[ "$rc" == "$want" ]]; then
    printf '  PASS  %-46s exit=%s\n' "$name" "$rc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL  %-46s exit=%s want=%s\n' "$name" "$rc" "$want"
    printf '        ---- output ----\n%s\n        ----------------\n' "$out"
    FAIL=$((FAIL + 1))
  fi
  rm -rf "$work"
}

echo "deploy.sh exit-code guards:"

# 正常路径：拉取成功且容器 revision 与镜像一致 -> 0
run_case "pull ok, revisions match" 0 yes "abc123" "abc123"

# ★ 本次修复的核心：两条链路都失败、回落旧镜像 -> 必须非零
run_case "pull failed, fell back to stale image" 1 no "abc123" "abc123"

# 同类第二个洞：拉取成功但容器没被重建（revision 不一致）-> 必须非零
run_case "pull ok, container still on old revision" 1 yes "old999" "new111"

echo
if [[ "$FAIL" -gt 0 ]]; then
  echo "✗ ${FAIL} failed, ${PASS} passed"
  exit 1
fi
echo "✓ all ${PASS} passed"
