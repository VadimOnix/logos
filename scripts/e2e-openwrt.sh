#!/usr/bin/env bash
# Manual E2E: a REAL OpenWrt rootfs container (real uci, real apk/opkg)
# enrolls into the control plane while Chromium drives the actual admin
# panel (scripts/e2e-panel.mjs). Run from CI via workflow_dispatch
# (.github/workflows/e2e.yml) or locally with docker + a Postgres:
#
#   E2E_DATABASE_URL=postgres://logos:logos@127.0.0.1:5432/logos ./scripts/e2e-openwrt.sh
#
# The OpenWrt container shares the host network namespace, so the agent
# dials the server on 127.0.0.1 exactly like the panel does.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
CONTAINER=logos-e2e-node
IMAGE="${E2E_OPENWRT_IMAGE:-openwrt/rootfs:x86_64-v24.10.7}"

cleanup() {
  docker rm -f "$CONTAINER" > /dev/null 2>&1 || true
  kill $(jobs -p) 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

DB="${E2E_DATABASE_URL:-postgres://logos:logos@127.0.0.1:5432/logos}"
API=http://127.0.0.1:18080

say()  { echo "== $*"; }
fail() { echo "E2E FAIL: $*" >&2; sed -n '1,60p' "$TMP"/*.log 2>/dev/null >&2 || true; exit 1; }

say "build server + static agent"
go build -o "$TMP/logos-server" "$ROOT/server/cmd/logos-server"
CGO_ENABLED=0 go build -o "$TMP/logos-agent" "$ROOT/agent/cmd/logos-agent"

say "start control plane"
LOGOS_DATABASE_URL="$DB" \
LOGOS_ADMIN_EMAIL=e2e@example.com \
LOGOS_ADMIN_PASSWORD=e2e-password-1 \
LOGOS_LISTEN=:18080 \
LOGOS_AGENT_LISTEN=:18443 \
LOGOS_LOG_LEVEL=warn \
"$TMP/logos-server" > "$TMP/server.log" 2>&1 &

for _ in $(seq 1 100); do
  curl -fsS "$API/readyz" > /dev/null 2>&1 && break
  sleep 0.3
done
curl -fsS "$API/readyz" > /dev/null || fail "server never became ready"

say "start OpenWrt rootfs container ($IMAGE)"
docker rm -f "$CONTAINER" > /dev/null 2>&1 || true
docker run -d --name "$CONTAINER" --network host \
  -v "$TMP/logos-agent:/usr/bin/logos-agent:ro" \
  "$IMAGE" sleep 2147483647 > /dev/null
# The rootfs image hasn't gone through first boot (no procd/uci-defaults),
# so /etc/config/system doesn't exist yet. Seed the minimal config the test
# touches — exactly what a first boot would have generated.
docker exec "$CONTAINER" sh -c \
  'mkdir -p /etc/config && printf "config system\n\toption hostname e2e-node\n" > /etc/config/system'
docker exec "$CONTAINER" uci get system.@system[0].hostname > /dev/null \
  || fail "container has no working uci — wrong image?"

# The panel script mints the claim code in the browser and calls this hook
# with CODE in the environment.
cat > "$TMP/enroll-hook.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
docker exec -e CODE="\$CODE" $CONTAINER sh -c \
  '/usr/bin/logos-agent enroll --server $API --code "\$CODE"'
docker exec -d $CONTAINER /usr/bin/logos-agent run --portal off
EOF
chmod +x "$TMP/enroll-hook.sh"

say "drive the admin panel in Chromium"
E2E_API="$API" \
E2E_EMAIL=e2e@example.com \
E2E_PASSWORD=e2e-password-1 \
E2E_ENROLL_HOOK="$TMP/enroll-hook.sh" \
E2E_ARTIFACTS="${E2E_ARTIFACTS:-$TMP/artifacts}" \
node "$ROOT/scripts/e2e-panel.mjs" || {
  echo "--- agent log (container) ---" >&2
  docker exec "$CONTAINER" sh -c 'cat /tmp/logos-agent.log 2>/dev/null || true' >&2
  fail "panel E2E failed"
}

say "E2E OK"
