#!/bin/sh
# Provisions OpenSearch Dashboards: index pattern, search, and dashboard.
# Idempotent: uses fixed IDs (POST with an id overwrites).
set -e

HOST="${DASHBOARDS_HOST:-http://localhost:5601}"

echo "waiting for dashboards at $HOST ..."
until curl -sf "$HOST/api/status" >/dev/null 2>&1; do
  sleep 3
done

echo "creating index pattern soc-events ..."
curl -sf -X POST "$HOST/api/saved_objects/index-pattern/soc-events?overwrite=true" \
  -H "Content-Type: application/json" -H "osd-xsrf: true" \
  -d '{"attributes":{"title":"soc-events","timeFieldName":"timestamp"}}' >/dev/null

echo "creating search SOC events ..."
curl -sf -X POST "$HOST/api/saved_objects/search/soc-events-search?overwrite=true" \
  -H "Content-Type: application/json" -H "osd-xsrf: true" \
  -d '{"attributes":{"title":"SOC events","description":"All SOC events","columns":["type","protocol","source","model","blocked","session_id","timestamp"],"sort":[["timestamp","desc"]],"kibanaSavedObjectMeta":{"searchSourceJSON":"{\"query\":{\"query\":\"\",\"language\":\"kuery\"},\"filter\":[],\"indexRefName\":\"kibanaSavedObjectMeta.searchSourceJSON.index\"}"}},"references":[{"name":"kibanaSavedObjectMeta.searchSourceJSON.index","type":"index-pattern","id":"soc-events"}]}' >/dev/null

echo "creating dashboard AI-SOC Overview ..."
curl -sf -X POST "$HOST/api/saved_objects/dashboard/soc-overview?overwrite=true" \
  -H "Content-Type: application/json" -H "osd-xsrf: true" \
  -d '{"attributes":{"title":"AI-SOC Overview","description":"SOC events in real time","timeRestore":false,"optionsJSON":"{\"useMargins\":true,\"hidePanelTitles\":false}","panelsJSON":"[{\"version\":\"2.19.1\",\"gridData\":{\"x\":0,\"y\":0,\"w\":48,\"h\":24,\"i\":\"1\"},\"panelIndex\":\"1\",\"embeddableConfig\":{},\"type\":\"search\",\"panelRefName\":\"panel_1\"}]"},"references":[{"name":"panel_1","type":"search","id":"soc-events-search"}]}' >/dev/null

echo "dashboards ready"
