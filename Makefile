GO ?= $(HOME)/.local/go/bin/go

.PHONY: up up-mock down down-v logs build test vet topic export-native transect ps

## Bring up the full stack (real mode: reads keys from .env)
up:
	docker compose up -d --build

## Bring up the stack in mock mode (no real keys)
up-mock:
	SOC_MOCK=1 docker compose up -d --build

down:
	docker compose down

## Tear down and delete volumes (Grafana/OpenSearch data)
down-v:
	docker compose down -v

logs:
	docker compose logs -f

build:
	$(GO) build ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

## Create the soc.events topic (normally redpanda-init does this on its own)
topic:
	docker exec soc-redpanda rpk topic create soc.events -p 1

## Export native transcripts (Hermes state.db + Claude) to OpenClaw
export-native:
	$(GO) run ./cmd/export-native

## Run Transect (DeepSeek Pro judge) over the demos in demo_openclaw/
transect:
	~/.venvs/transect/bin/python scripts/run-transect-all.py

ps:
	docker compose ps
