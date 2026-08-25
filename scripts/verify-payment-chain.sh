#!/usr/bin/env bash
# Reproducible payment-chain verification: throwaway SQLite DB, fresh server,
# mock upstream with a fixed usage block. Asserts the exact charges.
#
#   bash scripts/verify-payment-chain.sh
#
# Needs: node, curl, openssl, and a built binary. Set LODESTAR_BIN to reuse one;
# otherwise this builds to $WORK/lodestar-verify.
#
# Everything it writes lands in .tmp/payment-chain/ (gitignored). It never
# touches the real data/ directory and never talks to a real upstream.
set -uo pipefail

PORT="${PORT:-8123}"
MOCKPORT="${MOCKPORT:-8899}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$ROOT/.tmp/payment-chain"
cd "$ROOT"

mkdir -p "$WORK"

BIN="${LODESTAR_BIN:-}"
if [ -z "$BIN" ]; then
  BIN="$WORK/lodestar-verify"
  echo "--- building $BIN"
  mkdir -p static/out && touch static/out/.keep
  go build -o "$BIN" . || { echo "!!! build failed"; exit 1; }
fi

echo "--- stopping anything on our two ports ($PORT, $MOCKPORT)"
for p in "$PORT" "$MOCKPORT"; do
  if command -v netstat >/dev/null 2>&1 && [[ "${OS:-}" == Windows_NT ]]; then
    pid=$(netstat -ano 2>/dev/null | grep LISTENING | grep -E ":$p\b" | awk '{print $NF}' | head -1)
    [ -n "${pid:-}" ] && { echo "  killing pid $pid on port $p"; taskkill //PID "$pid" //F >/dev/null 2>&1; }
  else
    pid=$(lsof -ti ":$p" 2>/dev/null | head -1)
    [ -n "${pid:-}" ] && { echo "  killing pid $pid on port $p"; kill -9 "$pid" 2>/dev/null; }
  fi
done
sleep 2

echo "--- wiping throwaway state"
rm -rf "$WORK/data"; mkdir -p "$WORK/data"
rm -f "$WORK/upstream-calls.log" "$WORK/server.log" "$WORK/mock.out"

echo "--- starting mock upstream on $MOCKPORT"
WORK="$WORK" MOCK_PORT="$MOCKPORT" node "$ROOT/scripts/verify-payment-chain-upstream.mjs" \
  > "$WORK/mock.out" 2>&1 &
sleep 2

echo "--- starting lodestar on $PORT"
export LODESTAR_DATA_DIR="$WORK/data" \
       LODESTAR_SERVER_PORT="$PORT" \
       LODESTAR_SECURITY_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
       LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" \
       LODESTAR_DATABASE_TYPE=sqlite \
       LODESTAR_DATABASE_PATH="$WORK/data/verify.db"
# Must stay unset: it short-circuits the relay with a canned 200 and charges nothing.
unset LODESTAR_DEV_MOCK_SUCCESS
nohup "$BIN" start > "$WORK/server.log" 2>&1 &

echo "--- waiting for server"
code=""
for i in $(seq 1 60); do
  code=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/api/v1/bootstrap/status" || true)
  [ "$code" = "200" ] && { echo "  up after ${i}s"; break; }
  sleep 1
done
if [ "$code" != "200" ]; then
  echo "!!! server did not come up; log tail:"; tail -30 "$WORK/server.log"; exit 1
fi

echo "--- verifying payment chain"
BASE="http://127.0.0.1:$PORT" MOCK="http://127.0.0.1:$MOCKPORT" \
  node "$ROOT/scripts/verify-payment-chain.mjs"
rc=$?

echo
echo "--- upstream requests actually received (a refused request must NOT appear):"
grep -E '"method"' "$WORK/upstream-calls.log" 2>/dev/null || echo "  (none logged)"

exit $rc
