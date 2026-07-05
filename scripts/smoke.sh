#!/usr/bin/env bash
# End-to-end smoke test: a real control plane and a real agent, driven only
# through the public API. The router bits (uci/opkg) are PATH stubs, so this
# runs on any Linux box with a reachable Postgres:
#
#   SMOKE_DATABASE_URL=postgres://logos:logos@127.0.0.1:5432/logos ./scripts/smoke.sh
#
# Covered end to end: readiness probe, login, claim-code enrollment, mTLS
# agent channel + heartbeats, fleet stats, bulk package action, config-drift
# raise + accept, audit trail.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
cleanup() {
  kill $(jobs -p) 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

DB="${SMOKE_DATABASE_URL:-postgres://logos:logos@127.0.0.1:5432/logos}"
API=http://127.0.0.1:18080
JAR="$TMP/cookies"
CURL="curl -fsS -b $JAR -c $JAR"

say()  { echo "== $*"; }
fail() { echo "SMOKE FAIL: $*" >&2; sed -n '1,50p' "$TMP"/*.log 2>/dev/null >&2 || true; exit 1; }

say "build server + agent"
go build -o "$TMP/logos-server" "$ROOT/server/cmd/logos-server"
go build -o "$TMP/logos-agent" "$ROOT/agent/cmd/logos-agent"

say "stub uci/opkg onto PATH"
mkdir -p "$TMP/bin"
export UCI_FIXTURE="$TMP/uci.txt"
printf 'config system\n\toption hostname smoke\n' > "$UCI_FIXTURE"
cat > "$TMP/bin/uci" <<'STUB'
#!/bin/sh
# uci stub: `uci export` prints the fixture (mutate it to simulate drift);
# every other invocation succeeds silently.
[ "${1:-}" = export ] && exec cat "$UCI_FIXTURE"
exit 0
STUB
cat > "$TMP/bin/opkg" <<'STUB'
#!/bin/sh
[ "${1:-}" = list-installed ] && { echo "base-files - 1500"; exit 0; }
exit 0
STUB
# wg is only LookPath'd as an overlay prerequisite; key generation is
# in-process in the agent.
printf '#!/bin/sh\nexit 0\n' > "$TMP/bin/wg"
chmod +x "$TMP/bin/uci" "$TMP/bin/opkg" "$TMP/bin/wg"
export PATH="$TMP/bin:$PATH"

say "start control plane"
LOGOS_DATABASE_URL="$DB" \
LOGOS_ADMIN_EMAIL=smoke@example.com \
LOGOS_ADMIN_PASSWORD=smoke-password-1 \
LOGOS_LISTEN=:18080 \
LOGOS_AGENT_LISTEN=:18443 \
LOGOS_LOG_LEVEL=warn \
"$TMP/logos-server" > "$TMP/server.log" 2>&1 &

for _ in $(seq 1 100); do
  curl -fsS "$API/readyz" > /dev/null 2>&1 && break
  sleep 0.3
done
curl -fsS "$API/readyz" > /dev/null || fail "server never became ready"

say "login + claim code"
$CURL -X POST "$API/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"smoke@example.com","password":"smoke-password-1"}' > /dev/null
CODE=$($CURL -X POST "$API/api/v1/claim-codes" -H 'Content-Type: application/json' -d '{}' | jq -r .code)
[ -n "$CODE" ] && [ "$CODE" != null ] || fail "no claim code minted"

say "agent enroll + run (mTLS channel)"
"$TMP/logos-agent" enroll --server "$API" --code "$CODE" --state "$TMP/agent-state.json"
"$TMP/logos-agent" run --state "$TMP/agent-state.json" --portal off > "$TMP/agent.log" 2>&1 &

say "wait for the node to come online"
NODE=""
for _ in $(seq 1 60); do
  NODE=$($CURL "$API/api/v1/nodes" | jq -r '.[0] // empty | select(.status == "online") | .id')
  [ -n "$NODE" ] && break
  sleep 0.5
done
[ -n "$NODE" ] || fail "node never came online"

say "fleet stats reflect the node"
STATS=$($CURL "$API/api/v1/stats")
[ "$(jq .nodes.online <<< "$STATS")" = 1 ] && [ "$(jq .nodes.total <<< "$STATS")" = 1 ] \
  || fail "unexpected stats: $STATS"

say "bulk package update (canary path)"
BULK=$($CURL -X POST "$API/api/v1/nodes/packages/bulk" -H 'Content-Type: application/json' \
  -d "{\"action\":\"update\",\"node_ids\":[\"$NODE\"],\"canary\":1}")
[ "$(jq .ok_count <<< "$BULK")" = 1 ] || fail "bulk update failed: $BULK"

say "config drift raises after the fixture changes (next heartbeat, ~30s)"
printf 'config system\n\toption hostname changed-outside-logos\n' > "$UCI_FIXTURE"
DRIFT=""
for _ in $(seq 1 90); do
  DRIFT=$($CURL "$API/api/v1/nodes/$NODE" | jq -r '.config_drift // false')
  [ "$DRIFT" = true ] && break
  sleep 1
done
[ "$DRIFT" = true ] || fail "config drift never raised"

say "accepting the baseline clears the drift flag"
$CURL -X POST "$API/api/v1/nodes/$NODE/config/baseline" -H 'Content-Type: application/json' -d '{}' > /dev/null
[ "$($CURL "$API/api/v1/nodes/$NODE" | jq -r '.config_drift // false')" = false ] \
  || fail "drift flag survived baseline accept"

say "config template: create, render with vars, apply through the revert flow"
TPL=$($CURL -X POST "$API/api/v1/config-templates" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-tpl","changes":[{"op":"set","key":"system.@system[0].hostname","value":"${node.name}-${suffix}"}]}')
TPLID=$(jq -r .id <<< "$TPL")
[ -n "$TPLID" ] && [ "$TPLID" != null ] || fail "template not created: $TPL"

APPLY=$($CURL -X POST "$API/api/v1/config-templates/$TPLID/apply" -H 'Content-Type: application/json' \
  -d "{\"node_ids\":[\"$NODE\"],\"vars\":{\"suffix\":\"smoke\"},\"revert_timeout_sec\":30}")
[ "$(jq .ok_count <<< "$APPLY")" = 1 ] || fail "template apply failed: $APPLY"
CHANGE=$(jq -r '.results[0].change_id' <<< "$APPLY")

say "config change $CHANGE confirms over the live channel"
STATUS=""
for _ in $(seq 1 60); do
  STATUS=$($CURL "$API/api/v1/nodes/$NODE/config/changes" \
    | jq -r --argjson id "$CHANGE" '.[] | select(.id == $id) | .status')
  [ "$STATUS" = confirmed ] && break
  sleep 1
done
[ "$STATUS" = confirmed ] || fail "change never confirmed (status: $STATUS)"

say "overlay: create, join, first sync reports the device key"
OV=$($CURL -X POST "$API/api/v1/overlays" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-mesh","cidr":"100.90.0.0/24"}')
OVID=$(jq -r .id <<< "$OV")
[ -n "$OVID" ] && [ "$OVID" != null ] || fail "overlay not created: $OV"
$CURL -X POST "$API/api/v1/overlays/$OVID/members" -H 'Content-Type: application/json' \
  -d "{\"node_id\":\"$NODE\"}" > /dev/null

KEY=""
for _ in $(seq 1 40); do
  KEY=$($CURL "$API/api/v1/overlays" | jq -r --argjson id "$OVID" \
    '.[] | select(.id == $id) | .members[0] | select(.sync_error == null or .sync_error == "") | .public_key // empty')
  [ -n "$KEY" ] && break
  sleep 0.5
done
[ -n "$KEY" ] || fail "overlay member never synced a public key"

say "overlay DNS hosts file published by the agent"
HOSTS=""
for _ in $(seq 1 20); do
  HOSTS=$(cat "/tmp/hosts/logos$OVID" 2>/dev/null || true)
  [ -n "$HOSTS" ] && break
  sleep 0.5
done
grep -q "smoke-mesh.logos" <<< "$HOSTS" || fail "overlay hosts file missing or wrong: $HOSTS"

say "overlay delete tears down and unpublishes DNS"
$CURL -X DELETE "$API/api/v1/overlays/$OVID" > /dev/null
GONE=""
for _ in $(seq 1 30); do
  [ ! -e "/tmp/hosts/logos$OVID" ] && GONE=1 && break
  sleep 0.5
done
[ -n "$GONE" ] || fail "overlay hosts file survived overlay deletion"

say "audit trail recorded the session"
AUDIT=$($CURL "$API/api/v1/audit")
[ "$(jq length <<< "$AUDIT")" -ge 6 ] || fail "audit too short: $AUDIT"
for action in auth.login claimcode.create package.bulk_update config.baseline_accept template.create template.apply; do
  jq -e --arg a "$action" 'map(.action) | index($a) != null' <<< "$AUDIT" > /dev/null \
    || fail "audit missing $action"
done

say "SMOKE OK"
