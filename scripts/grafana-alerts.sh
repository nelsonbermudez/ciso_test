#!/bin/sh
# Provisions the SOC alerts in Grafana via the API (idempotent by uid, PUT).
# Requires Grafana running. admin/admin by default.
set -e

GF="${GRAFANA_URL:-http://localhost:3000}"
USER="${GRAFANA_ADMIN_USER:-admin}"
PASS="${GRAFANA_ADMIN_PASSWORD:-admin}"
FOLDER_UID="soc-dash"
DS_UID="soc-opensearch"

# Ensures the folder exists (dashboard provisioning creates it).
curl -s -u "$USER:$PASS" -X POST "$GF/api/folders" \
  -H "Content-Type: application/json" \
  -d "{\"uid\":\"$FOLDER_UID\",\"title\":\"AI-SOC\"}" >/dev/null || true

# Optional webhook contact point (only if a URL is present).
if [ -n "${GRAFANA_WEBHOOK_URL:-}" ]; then
  curl -s -u "$USER:$PASS" -X POST "$GF/api/v1/provisioning/contact-points" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"soc-webhook\",\"type\":\"webhook\",\"settings\":{\"url\":\"$GRAFANA_WEBHOOK_URL\",\"httpMethod\":\"POST\"}}" >/dev/null || true
fi

put_rule() {
  curl -s -u "$USER:$PASS" -X PUT "$GF/api/v1/provisioning/alert-rules/$1" \
    -H "Content-Type: application/json" -d "$2" >/dev/null
}

# Rule 1: secret blocked.
put_rule soc-block-secret '{
  "uid": "soc-block-secret",
  "title": "SOC: secret blocked",
  "ruleGroup": "soc",
  "folderUID": "soc-dash",
  "for": "1m",
  "noDataState": "OK",
  "execErrState": "Error",
  "condition": "C",
  "data": [
    {
      "refId": "A",
      "relativeTimeRange": { "from": 300, "to": 0 },
      "datasourceUid": "soc-opensearch",
      "model": {
        "query": "type:block",
        "metrics": [{ "id": "1", "type": "count" }],
        "bucketAggs": [{ "id": "2", "type": "date_histogram", "field": "timestamp", "settings": { "interval": "auto" } }],
        "timeField": "timestamp",
        "refId": "A"
      }
    },
    {
      "refId": "B",
      "relativeTimeRange": { "from": 300, "to": 0 },
      "datasourceUid": "__expr__",
      "model": { "type": "reduce", "expression": "A", "reducer": "sum", "refId": "B" }
    },
    {
      "refId": "C",
      "relativeTimeRange": { "from": 300, "to": 0 },
      "datasourceUid": "__expr__",
      "model": {
        "type": "threshold",
        "expression": "B",
        "conditions": [{ "evaluator": { "type": "gt", "params": [0] } }],
        "refId": "C"
      }
    }
  ],
  "annotations": { "summary": "The gateway blocked one or more secrets" },
  "labels": { "alerttype": "secret_block" }
}'

# Rule 2: exfiltration (findings in the out direction).
put_rule soc-exfil '{
  "uid": "soc-exfil",
  "title": "SOC: data exfiltration",
  "ruleGroup": "soc",
  "folderUID": "soc-dash",
  "for": "1m",
  "noDataState": "OK",
  "execErrState": "Error",
  "condition": "C",
  "data": [
    {
      "refId": "A",
      "relativeTimeRange": { "from": 300, "to": 0 },
      "datasourceUid": "soc-opensearch",
      "model": {
        "query": "exfil:true",
        "metrics": [{ "id": "1", "type": "count" }],
        "bucketAggs": [{ "id": "2", "type": "date_histogram", "field": "timestamp", "settings": { "interval": "auto" } }],
        "timeField": "timestamp",
        "refId": "A"
      }
    },
    {
      "refId": "B",
      "relativeTimeRange": { "from": 300, "to": 0 },
      "datasourceUid": "__expr__",
      "model": { "type": "reduce", "expression": "A", "reducer": "sum", "refId": "B" }
    },
    {
      "refId": "C",
      "relativeTimeRange": { "from": 300, "to": 0 },
      "datasourceUid": "__expr__",
      "model": {
        "type": "threshold",
        "expression": "B",
        "conditions": [{ "evaluator": { "type": "gt", "params": [0] } }],
        "refId": "C"
      }
    }
  ],
  "annotations": { "summary": "Sensitive data was detected leaving in a response" },
  "labels": { "alerttype": "exfiltration" }
}'

echo "alerts provisioned (soc-block-secret, soc-exfil)"
