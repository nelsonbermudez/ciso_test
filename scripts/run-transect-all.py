#!/usr/bin/env python3
"""Runs Transect with the DeepSeek Pro judge over all transcripts in
demo_openclaw/ (generates them if missing). Reads DEEPSEEK_API_KEY/BASE_URL from
the .env without exposing them.

Usage: ~/.venvs/transect/bin/python scripts/run-transect-all.py
Equivalent to `make transect`.
"""
import glob
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DIR = os.path.join(ROOT, "demo_openclaw")
REPORTS = os.path.join(ROOT, "reports")
SPEC = os.path.join(ROOT, "spec.yaml")

# Loads the .env (DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL) without printing it.
env = {}
envpath = os.path.join(ROOT, ".env")
if os.path.exists(envpath):
    for line in open(envpath):
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            env[k] = v

os.environ["OPENAI_BASE_URL"] = env.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1")
os.environ["OPENAI_API_KEY"] = env.get("DEEPSEEK_API_KEY", "")
if not os.environ["OPENAI_API_KEY"]:
    sys.exit("error: DEEPSEEK_API_KEY is not set in .env")

files = sorted(glob.glob(os.path.join(DIR, "*.jsonl")))
if not files:
    subprocess.run([sys.executable, os.path.join(ROOT, "scripts", "gen-demo.py")], check=True)
    files = sorted(glob.glob(os.path.join(DIR, "*.jsonl")))

from transect import transect  # noqa: E402  (tras configurar env)

os.makedirs(REPORTS, exist_ok=True)
for f in files:
    stem = os.path.splitext(os.path.basename(f))[0]
    # sample id = <stem>-<timestamp of the first message>
    ts = None
    for line in open(f):
        obj = json.loads(line)
        if obj.get("type") == "agent.end":
            msgs = obj.get("messages", [])
            if msgs and msgs[0].get("timestamp"):
                ts = msgs[0]["timestamp"]
                break
    sample = f"{stem}-{ts}" if ts is not None else stem
    out = os.path.join(REPORTS, f"{stem}-deepseek.html")
    r = transect(DIR, SPEC, sample=sample,
                 judge_models="openai/deepseek-v4-pro",
                 verify=False, viewer=False, report_path=out)
    print("OK", r.report_paths, flush=True)
