#!/usr/bin/env python3
"""Envía tráfico simulado al gateway (modo mock) para poblar el dashboard de
Grafana: actividad por agente, uso de modelos, PII y bloqueos (alarmas).

Uso: python3 scripts/simulate-traffic.py [gateway_url]
"""
import json
import sys
import urllib.error
import urllib.request

GW = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"


def post(path, body, session=None):
    data = json.dumps(body).encode()
    headers = {"Content-Type": "application/json"}
    if session:
        headers["X-Session-ID"] = session
    req = urllib.request.Request(GW + path, data=data, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code


def openai(session, model, content):
    return post("/v1/chat/completions",
                {"model": model, "messages": [{"role": "user", "content": content}]},
                session)


def anthropic(session, model, content):
    return post("/v1/messages",
                {"model": model, "max_tokens": 100,
                 "messages": [{"role": "user", "content": content}]},
                session)


# ---- Tráfico limpio (agentes + modelos) ----
clean = [
    ("demo-marketing", "deepseek-v4-pro", "Redacta el copy para la sección hero de la landing page."),
    ("demo-marketing", "deepseek-v4-pro", "Sugiere tres títulos para el blog de Nimbus."),
    ("demo-marketing", "deepseek-v4-pro", "Traduce este párrafo al inglés manteniendo el tono."),
    ("demo-backend", "deepseek-flash", "Genera un endpoint /users con net/http en Go."),
    ("demo-backend", "deepseek-flash", "¿Cómo implemento un healthcheck en Go?"),
    ("demo-backend", "deepseek-flash", "Explica el patrón repository en Go."),
    ("claude-frontend", "claude-3-opus", "Refactoriza este componente React para que sea responsive."),
    ("claude-frontend", "claude-3-opus", "Añade soporte de tema oscuro con Tailwind."),
    ("claude-frontend", "claude-3-opus", "¿Cómo manejo estado global en una app React?"),
    ("demo-backend", "deepseek-flash", "Dame un ejemplo de middleware de logging en Go."),
    ("demo-marketing", "deepseek-v4-pro", "Escribe un hilo de X anunciando el lanzamiento."),
    ("claude-frontend", "claude-3-opus", "Optimiza el bundle con code-splitting."),
]

# ---- PII (se registra, no bloquea) ----
pii = [
    ("analista-datos", "deepseek-v4-pro", "Contacta al cliente juan.perez@empresa.com por el incidente."),
    ("analista-datos", "deepseek-v4-pro", "Escala al soporte llamando al +57 310 555 1234."),
    ("analista-datos", "deepseek-v4-pro", "El servidor 192.168.1.80 no responde al healthcheck."),
    ("analista-datos", "deepseek-v4-pro", "La tarjeta de prueba es 4111111111111111 para el pago."),
    ("claude-frontend", "claude-3-opus", "Registra admin@nimbus.io como correo de contacto."),
    ("analista-datos", "deepseek-flash", "IBAN de la cuenta: GB82WEST12345698765432."),
]

# ---- Secrets (bloquean -> alarmas) ----
secrets = [
    ("demo-seguridad", "deepseek-v4-pro", "mi api key es sk-abcdefghijklmnopqrstuvwxyz123456"),
    ("demo-seguridad", "deepseek-v4-pro", "password=supersecreto123"),
    ("claude-frontend", "claude-3-opus", "el token es ghp_abcdefghijklmnopqrstuvwxyz1234567890"),
]

total = 0
for session, model, content in clean:
    openai(session, model, content) if model.startswith("deepseek") else anthropic(session, model, content)
    total += 1
for session, model, content in pii:
    openai(session, model, content) if model.startswith("deepseek") else anthropic(session, model, content)
    total += 1
for session, model, content in secrets:
    openai(session, model, content) if model.startswith("deepseek") else anthropic(session, model, content)
    total += 1

print(f"enviados {total} requests al gateway ({GW})")
print("  limpios:", len(clean), "| PII:", len(pii), "| secrets (bloqueos):", len(secrets))
print("  revisa Grafana: http://192.168.1.80:3000 (dashboard AI-SOC Overview)")
