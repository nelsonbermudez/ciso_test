#!/usr/bin/env python3
"""Generates demo OpenClaw transcripts of a web page build
for Hermes (DeepSeek) and Claude Code, with multiple turns and tool calls that
show the flow: explore -> plan -> implement -> test -> verify.

Usage: ~/.venvs/transect/bin/python scripts/gen-demo.py
Writes: demo_openclaw/sim-hermes-demo.jsonl and demo_openclaw/sim-claude-demo.jsonl
"""
import json
import os
import time

OUT = os.path.join(os.path.dirname(__file__), "..", "demo_openclaw")
OUT = os.path.abspath(OUT)
os.makedirs(OUT, exist_ok=True)


def msg_user(key, ts, text):
    return {"role": "user", "idempotencyKey": key, "timestamp": ts, "content": text}


def msg_toolresult(ts, call_id, name, text, is_error=False):
    return {"role": "toolResult", "toolCallId": call_id, "toolName": name,
            "timestamp": ts, "isError": is_error, "content": text}


def msg_assistant(ts, key, api, provider, model, content, stop="stop"):
    return {"role": "assistant", "responseId": key, "timestamp": ts,
            "api": api, "provider": provider, "model": model,
            "responseModel": model, "stopReason": stop, "content": content}


def assistant_with_tool(ts, key, api, provider, model, text, call_id, name, args):
    blocks = []
    if text:
        blocks.append({"type": "text", "text": text})
    blocks.append({"type": "toolCall", "id": call_id, "name": name, "arguments": args})
    return msg_assistant(ts, key, api, provider, model, blocks, stop="toolUse")


def write_session(path, sid, msgs):
    sk = "agent:main:main:" + sid
    with open(path, "w") as f:
        f.write(json.dumps({"type": "agent.start", "sessionKey": sk,
                            "agentId": "main", "messages": []}) + "\n")
        f.write(json.dumps({"type": "agent.end", "sessionKey": sk,
                            "agentId": "main", "messages": msgs}) + "\n")


BASE = int(time.time() * 1000)

# ---------- DeepSeek (Hermes): SaaS landing page ----------
t = BASE
h = []
h.append(msg_user("h1", t, "Create a landing page for the SaaS 'Nimbus'. It must have a hero section, a section with 3 features, and a contact form.")); t += 1
h.append(assistant_with_tool(t, "h2", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "First I check the project structure to find out where the static files go.",
    "call-h1", "list_files", {"path": "."})); t += 1
h.append(msg_toolresult(t, "call-h1", "list_files",
    "index.html (empty)\ncss/\njs/\npackage.json")); t += 1
h.append(msg_assistant(t, "h3", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "Plan: create index.html with hero + features + form, css/styles.css with a responsive design, and js/app.js with the form validation.")); t += 1
h.append(assistant_with_tool(t, "h4", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "I create index.html with the three sections.",
    "call-h2", "write_file", {"path": "index.html", "content": "<section class='hero'>…</section><section class='features'>…</section><form id='contact'>…</form>"})); t += 1
h.append(msg_toolresult(t, "call-h2", "write_file", "index.html written (hero, features, form)")); t += 1
h.append(assistant_with_tool(t, "h5", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "I add the responsive styles.",
    "call-h3", "write_file", {"path": "css/styles.css", "content": ".hero{…} .features{display:grid} … media queries"})); t += 1
h.append(msg_toolresult(t, "call-h3", "write_file", "css/styles.css written")); t += 1
h.append(assistant_with_tool(t, "h6", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "I add the contact form validation.",
    "call-h4", "write_file", {"path": "js/app.js", "content": "form.addEventListener('submit', validateEmail)"})); t += 1
h.append(msg_toolresult(t, "call-h4", "write_file", "js/app.js written")); t += 1
h.append(assistant_with_tool(t, "h7", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "I run a quick validation of the HTML and the build.",
    "call-h5", "terminal", {"command": "npm run build && npx html-validate index.html"})); t += 1
h.append(msg_toolresult(t, "call-h5", "terminal", "build ok — no HTML errors")); t += 1
h.append(msg_assistant(t, "h8", "openai-chat-completions", "deepseek", "deepseek-v4-pro",
    "Done. Nimbus landing page complete: hero, 3 features, and a form with validation. Verified in the browser."))

# ---------- Claude Code: gallery with lightbox ----------
t = BASE
c = []
c.append(msg_user("c1", t, "Add an image gallery with a lightbox to the landing page (click to enlarge and close).")); t += 1
c.append(assistant_with_tool(t, "c2", "anthropic-messages", "anthropic", "claude-3-opus",
    "I review the current index.html to see where the gallery fits.",
    "call-c1", "read_file", {"path": "index.html"})); t += 1
c.append(msg_toolresult(t, "call-c1", "read_file",
    "… <section class='features'> … </section> <form id='contact'> …")); t += 1
c.append(msg_assistant(t, "c3", "anthropic-messages", "anthropic", "claude-3-opus",
    "Plan: insert the gallery markup between features and the form, create css/lightbox.css and js/lightbox.js to open/close on click.")); t += 1
c.append(assistant_with_tool(t, "c4", "anthropic-messages", "anthropic", "claude-3-opus",
    "I insert the gallery into index.html.",
    "call-c2", "patch", {"path": "index.html", "change": "<section class='gallery'>…images…</section>"})); t += 1
c.append(msg_toolresult(t, "call-c2", "patch", "gallery inserted into index.html")); t += 1
c.append(assistant_with_tool(t, "c5", "anthropic-messages", "anthropic", "claude-3-opus",
    "I create the lightbox styles.",
    "call-c3", "write_file", {"path": "css/lightbox.css", "content": ".lightbox{position:fixed;inset:0;display:none}"})); t += 1
c.append(msg_toolresult(t, "call-c3", "write_file", "css/lightbox.css written")); t += 1
c.append(assistant_with_tool(t, "c6", "anthropic-messages", "anthropic", "claude-3-opus",
    "I create the JS that opens and closes the lightbox.",
    "call-c4", "write_file", {"path": "js/lightbox.js", "content": "gallery.addEventListener('click', openLightbox)"})); t += 1
c.append(msg_toolresult(t, "call-c4", "write_file", "js/lightbox.js written")); t += 1
c.append(assistant_with_tool(t, "c7", "anthropic-messages", "anthropic", "claude-3-opus",
    "I verify that the page didn't break anything.",
    "call-c5", "terminal", {"command": "npm run build"})); t += 1
c.append(msg_toolresult(t, "call-c5", "terminal", "build ok")); t += 1
c.append(msg_assistant(t, "c8", "anthropic-messages", "anthropic", "claude-3-opus",
    "Gallery with lightbox added and working: click enlarges, click outside closes. Verified."))

write_session(os.path.join(OUT, "sim-hermes-demo.jsonl"), "sim-hermes-demo", h)
write_session(os.path.join(OUT, "sim-claude-demo.jsonl"), "sim-claude-demo", c)
print("generated in", OUT)
print(" - sim-hermes-demo.jsonl (", len(h), "messages)")
print(" - sim-claude-demo.jsonl (", len(c), "messages)")
