#!/bin/sh
# Runs Transect with the DeepSeek Pro judge (deepseek-v4-pro).
# Reads DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL from the project's .env (gitignored),
# the same mechanism the gateway uses. Does not print the key.
#
# Usage: sh scripts/run-transect-deepseek.sh <dir_jsonl> <sample-id> [report_path]
#   e.g. sh scripts/run-transect-deepseek.sh demo_openclaw sim-hermes-demo-1792547366000 reports/sim-hermes-deepseek.html
set -e
cd "$(dirname "$0")/.."

DIR="${1:-demo_openclaw}"
SAMPLE="${2:?usage: run-transect-deepseek.sh <dir_jsonl> <sample-id> [report_path]}"
REPORT="${3:-reports/${SAMPLE}-deepseek.html}"

# Loads the .env without exposing the values.
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi

if [ -z "${DEEPSEEK_API_KEY:-}" ]; then
  echo "error: DEEPSEEK_API_KEY is not set in .env" >&2
  exit 1
fi

# Inspect (Transect's runner) resolves the openai provider from these vars.
export OPENAI_BASE_URL="${DEEPSEEK_BASE_URL:-https://api.deepseek.com/v1}"
export OPENAI_API_KEY="${DEEPSEEK_API_KEY}"

~/.venvs/transect/bin/python - "$DIR" "$SAMPLE" "$REPORT" <<'PY'
import sys
from transect import transect
d, sample, report = sys.argv[1], sys.argv[2], sys.argv[3]
r = transect(d, "spec.yaml", sample=sample,
             judge_models="openai/deepseek-v4-pro",
             verify=False, viewer=False, report_path=report)
print("OK", r.report_paths)
PY
