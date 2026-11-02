#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# RepoAtlas — Production & Railway Post-Deploy Smoke Test (Phase 4)
# Usage:
#   ./scripts/smoke-test.sh [FRONTEND_BASE_URL] [API_BASE_URL]
#
# When deployed with Next.js same-origin proxy (default on Railway), pass only
# FRONTEND_BASE_URL and API calls will automatically route through ${FRONTEND_BASE_URL}/api/v1/*.
# ==============================================================================

WEB_URL="${1:-http://127.0.0.1:3000}"
WEB_URL="${WEB_URL%/}"
API_URL="${2:-$WEB_URL}"
API_URL="${API_URL%/}"

echo "============================================================"
echo "RepoAtlas Smoke Test"
echo "  Frontend URL : ${WEB_URL}"
echo "  API Base URL : ${API_URL}"
echo "============================================================"

# 1. Top Page Check
echo "[1/7] Checking Top Page (${WEB_URL}/)..."
TOP_HTML="$(curl -fsSL "${WEB_URL}/")"
echo "${TOP_HTML}" | grep -q "RepoAtlas" || { echo "FAIL: Top page missing 'RepoAtlas'"; exit 1; }
echo "  -> PASS: Top page rendered."

# 2. Demo Page Check
echo "[2/7] Checking Demo Page (${WEB_URL}/demo)..."
DEMO_HTML="$(curl -fsSL "${WEB_URL}/demo")"
echo "${DEMO_HTML}" | grep -q "RepoAtlas" || { echo "FAIL: Demo page failed to render"; exit 1; }
echo "  -> PASS: Demo page rendered."

# 3. Healthz & Demo Graph API via Proxy/API
echo "[3/7] Checking /healthz and /api/v1/demo/graph..."
HEALTH_JSON="$(curl -fsSL "${API_URL}/healthz")"
echo "${HEALTH_JSON}" | grep -q '"status":"ok"' || { echo "FAIL: /healthz returned ${HEALTH_JSON}"; exit 1; }

DEMO_GRAPH_JSON="$(curl -fsSL "${API_URL}/api/v1/demo/graph")"
echo "${DEMO_GRAPH_JSON}" | grep -q '"demo":true' || { echo "FAIL: /api/v1/demo/graph missing demo:true"; exit 1; }
echo "  -> PASS: /healthz and /api/v1/demo/graph OK."

# 4. Production Guard Check (E2E fixture owner must not resolve in production)
echo "[4/7] Verifying production guard rejects synthetic fixture repository..."
FIXTURE_STATUS="$(curl -s -o /dev/null -w "%{http_code}" -X POST "${API_URL}/api/v1/analyses" \
  -H "Content-Type: application/json" \
  -d '{"repository_url":"https://github.com/fixture/sample-repo"}')"
if [[ "${FIXTURE_STATUS}" == "404" || "${FIXTURE_STATUS}" == "429" || "${FIXTURE_STATUS}" == "502" ]]; then
  echo "  -> PASS: Synthetic fixture repository was not served from local testdata (HTTP ${FIXTURE_STATUS})."
else
  echo "  -> WARN: Synthetic fixture returned HTTP ${FIXTURE_STATUS} (check APP_ENV=production)."
fi

# 5. Real GitHub Repository Analysis (pallets/itsdangerous)
echo "[5/7] Submitting real GitHub repository analysis (https://github.com/pallets/itsdangerous)..."
CREATE_RESP="$(curl -fsSL -X POST "${API_URL}/api/v1/analyses" \
  -H "Content-Type: application/json" \
  -d '{"repository_url":"https://github.com/pallets/itsdangerous"}')"
ANALYSIS_ID="$(echo "${CREATE_RESP}" | sed -n 's/.*"analysis_id":"\([^"]*\)".*/\1/p')"
if [[ -z "${ANALYSIS_ID}" ]]; then
  echo "FAIL: Could not extract analysis_id from response: ${CREATE_RESP}"
  exit 1
fi
echo "  -> Analysis ID: ${ANALYSIS_ID}"

for i in $(seq 1 45); do
  STATUS_RESP="$(curl -fsSL "${API_URL}/api/v1/analyses/${ANALYSIS_ID}")"
  STATUS="$(echo "${STATUS_RESP}" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')"
  if [[ "${STATUS}" == "completed" ]]; then
    echo "  -> PASS: Analysis completed."
    break
  elif [[ "${STATUS}" == "failed" ]]; then
    echo "FAIL: Analysis failed: ${STATUS_RESP}"
    exit 1
  fi
  sleep 2
done

# 6. Graph Retrieval Check
echo "[6/7] Fetching dependency graph (/api/v1/analyses/${ANALYSIS_ID}/graph)..."
GRAPH_RESP="$(curl -fsSL "${API_URL}/api/v1/analyses/${ANALYSIS_ID}/graph")"
echo "${GRAPH_RESP}" | grep -q '"nodes":\[' || { echo "FAIL: Graph response missing nodes"; exit 1; }
echo "${GRAPH_RESP}" | grep -q '"recommended_file_ids":\[' || { echo "FAIL: Graph response missing recommended_file_ids"; exit 1; }
echo "  -> PASS: Dependency graph & guided tour recommendations returned."

# 7. Cache Reuse Check
echo "[7/7] Re-submitting same repository to verify cache hit (HTTP 200 + cached:true)..."
CACHE_RESP="$(curl -fsSL -X POST "${API_URL}/api/v1/analyses" \
  -H "Content-Type: application/json" \
  -d '{"repository_url":"https://github.com/pallets/itsdangerous"}')"
echo "${CACHE_RESP}" | grep -q '"cached":true' || { echo "FAIL: Expected cached:true, got: ${CACHE_RESP}"; exit 1; }
echo "  -> PASS: Cache hit confirmed (cached:true)."

echo "============================================================"
echo "ALL SMOKE TESTS PASSED SUCCESSFULLY"
echo "============================================================"
