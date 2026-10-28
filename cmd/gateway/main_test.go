package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aisoc/internal/events"
)

func TestGatewayBlocksSecretAndForwardsClean(t *testing.T) {
	// Upstream mock que devuelve una respuesta fija (OpenAI).
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hola desde el mock"}}]}`))
	}))
	defer up.Close()

	t.Setenv("DEEPSEEK_BASE_URL", up.URL)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", up.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SOC_MOCK", "")
	t.Setenv("REDPANDA_BROKERS", "")

	g := newGateway()
	srv := httptest.NewServer(newMux(g))
	defer srv.Close()

	// 1) Petición con secret -> bloqueo 400.
	secretBody := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"mi api key es sk-123456789012345678901234567890"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(secretBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("se esperaba 400 por bloqueo, got %d", resp.StatusCode)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	resp.Body.Close()
	if errBody.Error.Code != "soc_blocked" {
		t.Fatalf("respuesta de bloqueo inesperada: %+v", errBody)
	}

	// 2) Petición limpia -> se reenvía y devuelve 200.
	cleanBody := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hola, hazme un resumen"}]}`
	resp2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(cleanBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("se esperaba 200, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestGatewayAnthropicBlockFormat(t *testing.T) {
	t.Setenv("SOC_MOCK", "1")
	t.Setenv("REDPANDA_BROKERS", "")

	g := newGateway()
	srv := httptest.NewServer(newMux(g))
	defer srv.Close()

	body := `{"model":"claude-3-opus","messages":[{"role":"user","content":"password=hunter2secret"}]}`
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("se esperaba 400, got %d", resp.StatusCode)
	}
	var errBody struct {
		Type  string `json:"type"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	resp.Body.Close()
	if errBody.Type != "error" {
		t.Fatalf("formato Anthropic de bloqueo inesperado: %+v", errBody)
	}
}

type captureSink struct{ ch chan events.Event }

func (c *captureSink) Emit(e events.Event) error { c.ch <- e; return nil }

func TestGatewayResponseExfil(t *testing.T) {
	// Upstream que devuelve un secret en la respuesta (exfiltración).
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"aquí va una key: password=hunter2secret"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer up.Close()

	t.Setenv("DEEPSEEK_BASE_URL", up.URL)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("SOC_MOCK", "")
	t.Setenv("REDPANDA_BROKERS", "")

	g := newGateway()
	cap := &captureSink{ch: make(chan events.Event, 10)}
	g.sink = cap
	srv := httptest.NewServer(newMux(g))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hola"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, se esperaba 200", resp.StatusCode)
	}

	// El evento response debe marcar Exfil=true y traer usage.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-cap.ch:
			if e.Type == "response" && e.Exfil && e.Usage != nil {
				return
			}
		case <-deadline:
			t.Fatal("no se detectó exfiltración ni usage en la respuesta")
		}
	}
}
