#!/usr/bin/env sh
# Smoke-test the running API. Override BASE_URL if needed, e.g.
#   BASE_URL=http://localhost:8080 ./scripts/smoke.sh
set -eu

BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "== health =="
curl -fsS "$BASE_URL/healthz"; echo

echo "== consistent triangle =="
curl -fsS -X POST "$BASE_URL/solve" \
  -H 'Content-Type: application/json' \
  -d '{
    "standards": ["a", "b", "c"],
    "comparisons": [
      {"id": "e1", "from": "a", "to": "b", "delta": 2},
      {"id": "e2", "from": "b", "to": "c", "delta": 3},
      {"id": "e3", "from": "a", "to": "c", "delta": 5}
    ]
  }'; echo

echo "== sign error on the closing record =="
curl -fsS -X POST "$BASE_URL/solve" \
  -H 'Content-Type: application/json' \
  -d '{
    "standards": ["a", "b", "c"],
    "comparisons": [
      {"id": "e1", "from": "a", "to": "b", "delta": 2},
      {"id": "e2", "from": "b", "to": "c", "delta": 3},
      {"id": "e3", "from": "a", "to": "c", "delta": -5}
    ]
  }'; echo

echo "== structural error (422) =="
curl -sS -o /dev/null -w "%{http_code}\n" -X POST "$BASE_URL/solve" \
  -H 'Content-Type: application/json' \
  -d '{"standards": ["a", "b"], "comparisons": [
        {"id": "x", "from": "a", "to": "ghost", "delta": 1}]}'
