#!/usr/bin/env bash
# Lodestar 服务器验收：热力图字段 +（可选）PG 日志库
# 用法：
#   export BASE="http://127.0.0.1:8080"
#   export TOKEN="<登录后 JWT 或 Bearer>"
#   bash scripts/verify-heatmap-server.sh
set -euo pipefail
BASE="${BASE:-http://127.0.0.1:8080}"
TOKEN="${TOKEN:-}"

echo "== bootstrap =="
curl -sS "$BASE/api/v1/bootstrap/status" | head -c 500
echo ""

if [[ -z "$TOKEN" ]]; then
  echo "TOKEN 未设，跳过 /api/v1/wallet/usage（需登录）。"
  echo "登录后：export TOKEN=\$(curl -sS -X POST .../api/v1/user/login -H 'Content-Type: application/json' -d '{\"username\":\"...\",\"password\":\"...\"}' | jq -r '.data.token')"
  exit 0
fi

echo "== wallet usage (heatmap_by_day) =="
BODY=$(curl -sS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/wallet/usage?days=14")
echo "$BODY" | head -c 1200
echo ""

if echo "$BODY" | grep -q 'heatmap_by_day'; then
  echo "OK: 响应含 heatmap_by_day"
else
  echo "FAIL: 无 heatmap_by_day（确认已部署 commit >= 63eb38f）"
  exit 1
fi

if echo "$BODY" | grep -q '"usage_chart_available":true'; then
  echo "OK: usage_chart_available=true（需设置 relay_log_keep_enabled + 有日志库）"
else
  echo "NOTE: usage_chart_available 非 true — 在 设置 中开启「保留 relay 日志」并确认 log DB 可用"
fi