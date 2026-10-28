# AI-SOC (ciso_test)

SOC for observing and auditing LLM consumption. A single gateway sits between
your LLM clients (Hermes / Claude Code) and the upstream APIs: it blocks secrets
before they leave, and logs PII, tokens, and exfiltration for analysis in
Grafana.

## Flow

```
Hermes (DeepSeek)             Claude Code (Anthropic)
      |   /v1/chat/completions       |   /v1/messages
      +------------------------------+----------------+
                             v
                       gateway (Go)    <- blocks secrets, logs everything
                       /          \
                      v            v
               DeepSeek API    Anthropic API
                      |
                      v   (events)
      Redpanda -> ingest -> OpenSearch -> Grafana
                      \-> transect -> report.html
```

## Usage

```bash
cd ~/Devs/ciso_test
make up          # real mode (reads .env)
make up-mock     # mock mode, no keys
```

Point Hermes `base_url` to `http://localhost:8080/v1`, and Claude Code
`ANTHROPIC_BASE_URL` to `http://localhost:8080`.

- Grafana: http://localhost:3000
- Transect viewer: http://localhost:8090

Only secrets block (400 `soc_blocked`); PII is logged without blocking.
