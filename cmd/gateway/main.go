// Command gateway: the SOC choke point. It exposes /v1/chat/completions (OpenAI,
// for Hermes) and /v1/messages (Anthropic, for Claude Code). It runs DLP before and
// after, blocks or forwards, and emits telemetry (Redpanda or log).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"aisoc/internal/broker"
	"aisoc/internal/config"
	"aisoc/internal/dlp"
	"aisoc/internal/events"
	"aisoc/internal/extract"
	"aisoc/internal/upstream"
)

// sink is the destination for telemetry events.
type sink interface {
	Emit(events.Event) error
}

type logSink struct{}

func (logSink) Emit(e events.Event) error {
	b, _ := json.Marshal(e)
	log.Printf("[event] %s", b)
	return nil
}

type redpandaSink struct {
	client *kgo.Client
	ch     chan events.Event
}

func newRedpandaSink(client *kgo.Client) *redpandaSink {
	s := &redpandaSink{client: client, ch: make(chan events.Event, 1024)}
	go s.run()
	return s
}

// Emit is non-blocking: it queues the event and returns. Telemetry must never
// slow the security decision path (block/forward).
func (r *redpandaSink) Emit(e events.Event) error {
	select {
	case r.ch <- e:
		return nil
	default:
		log.Printf("warning: event buffer full, dropping %s", e.Type)
		return nil
	}
}

func (r *redpandaSink) run() {
	for e := range r.ch {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = broker.Produce(ctx, r.client, []byte(e.RequestID), b)
		cancel()
		if err != nil {
			log.Printf("warning: emit %s: %v", e.Type, err)
		}
	}
}

type upstreamTarget struct {
	baseURL string
	apiKey  string
	path    string
}

type gateway struct {
	sink      sink
	fwd       *upstream.Forwarder
	deepseek  upstreamTarget
	anthropic upstreamTarget
	mock      bool
}

func newGateway() *gateway {
	g := &gateway{
		fwd: upstream.New(),
		deepseek: upstreamTarget{
			baseURL: strings.TrimRight(config.Get("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"), "/"),
			apiKey:  config.Get("DEEPSEEK_API_KEY", ""),
			path:    "/chat/completions",
		},
		anthropic: upstreamTarget{
			baseURL: strings.TrimRight(config.Get("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1"), "/"),
			apiKey:  config.Get("ANTHROPIC_API_KEY", ""),
			path:    "/messages",
		},
		mock: config.Bool("SOC_MOCK"),
	}

	brokers := config.List("REDPANDA_BROKERS")
	topic := config.Get("REDPANDA_TOPIC", "soc.events")
	if len(brokers) == 0 {
		g.sink = logSink{}
	} else {
		c, err := broker.NewProducer(brokers, topic)
		if err != nil {
			log.Printf("warning: could not create Redpanda producer (%v); using log", err)
			g.sink = logSink{}
		} else {
			g.sink = newRedpandaSink(c)
		}
	}
	return g
}

func newMux(g *gateway) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", g.handleChatCompletions)
	mux.HandleFunc("/v1/messages", g.handleMessages)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	return mux
}

func main() {
	g := newGateway()
	addr := config.Get("GATEWAY_ADDR", ":8080")
	log.Printf("gateway listening on %s (mock=%v)", addr, g.mock)
	if err := http.ListenAndServe(addr, newMux(g)); err != nil {
		log.Fatal(err)
	}
}

func (g *gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	g.handle(w, r, "openai", "hermes", g.deepseek)
}

func (g *gateway) handleMessages(w http.ResponseWriter, r *http.Request) {
	g.handle(w, r, "anthropic", "claude_code", g.anthropic)
}

func (g *gateway) handle(w http.ResponseWriter, r *http.Request, protocol, source string, up upstreamTarget) {
	start := time.Now()
	requestID := newID()
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-ID"))
	if sessionID == "" {
		sessionID = source
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	streaming := isStreaming(body)
	text, model, err := extract.Request(body)
	if err != nil || strings.TrimSpace(text) == "" {
		text = string(body)
	}

	findings := dlp.Scan(text)

	reqEv := events.New("request")
	reqEv.RequestID = requestID
	reqEv.Source = source
	reqEv.Protocol = protocol
	reqEv.Model = model
	reqEv.Direction = "in"
	reqEv.Blocked = dlp.ShouldBlock(findings)
	reqEv.Findings = findings
	reqEv.TextLen = len(text)
	reqEv.SessionID = sessionID
	reqEv.Content = dlp.Redact(text, findings)
	reqEv.FindingCategories = dlp.Categories(findings)
	reqEv.FindingRules = dlp.Rules(findings)
	g.emit(reqEv)

	if dlp.ShouldBlock(findings) {
		blk := events.New("block")
		blk.RequestID = requestID
		blk.Source = source
		blk.Protocol = protocol
		blk.Model = model
		blk.Blocked = true
		blk.Findings = findings
		blk.LatencyMS = time.Since(start).Milliseconds()
		blk.SessionID = sessionID
		blk.Content = dlp.Redact(text, findings)
		blk.FindingCategories = dlp.Categories(findings)
		blk.FindingRules = dlp.Rules(findings)
		g.emit(blk)
		writeBlock(w, protocol, findings)
		return
	}

	var status int
	var respBody []byte
	var contentType string
	if g.mock {
		status, respBody, contentType = mockResponse(protocol, model)
	} else {
		target := up.baseURL + up.path
		status, respBody, contentType, err = g.fwd.Forward(r.Context(), target, upstreamHeaders(protocol, up.apiKey, r.Header), body)
		if err != nil {
			http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
			return
		}
	}

	respText := extract.ResponseText(protocol, respBody, streaming)
	respFindings := dlp.Scan(respText)
	promptTokens, completionTokens, totalTokens := extract.Usage(protocol, respBody)

	respEv := events.New("response")
	respEv.RequestID = requestID
	respEv.Source = source
	respEv.Protocol = protocol
	respEv.Model = model
	respEv.Direction = "out"
	respEv.Findings = respFindings
	respEv.UpstreamStatus = status
	respEv.LatencyMS = time.Since(start).Milliseconds()
	respEv.TextLen = len(respText)
	respEv.SessionID = sessionID
	if totalTokens > 0 || promptTokens > 0 || completionTokens > 0 {
		respEv.Usage = &events.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		}
	}
	respEv.Content = dlp.Redact(respText, respFindings)
	respEv.FindingCategories = dlp.Categories(respFindings)
	respEv.FindingRules = dlp.Rules(respFindings)
	respEv.Exfil = len(respFindings) > 0
	g.emit(respEv)

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	w.Write(respBody)
}

func (g *gateway) emit(e events.Event) {
	if err := g.sink.Emit(e); err != nil {
		log.Printf("warning: emit %s: %v", e.Type, err)
	}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isStreaming(body []byte) bool {
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		if v, ok := m["stream"].(bool); ok {
			return v
		}
	}
	return false
}

func upstreamHeaders(protocol, apiKey string, incoming http.Header) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	if protocol == "openai" {
		h.Set("Authorization", "Bearer "+apiKey)
	} else {
		h.Set("x-api-key", apiKey)
		if v := incoming.Get("anthropic-version"); v != "" {
			h.Set("anthropic-version", v)
		} else {
			h.Set("anthropic-version", "2023-06-01")
		}
	}
	return h
}

func writeBlock(w http.ResponseWriter, protocol string, findings []dlp.Finding) {
	msg := fmt.Sprintf("Blocked by the SOC: secrets detected (%s)", strings.Join(ruleNames(findings), ", "))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if protocol == "openai" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": msg,
				"type":    "invalid_request_error",
				"code":    "soc_blocked",
			},
		})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": msg,
			},
		})
	}
}

func ruleNames(fs []dlp.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if !seen[f.Rule] {
			seen[f.Rule] = true
			out = append(out, f.Rule)
		}
	}
	return out
}

func mockResponse(protocol, model string) (int, []byte, string) {
	ct := "application/json"
	if protocol == "openai" {
		body, _ := json.Marshal(map[string]any{
			"id": "mock", "object": "chat.completion", "model": model,
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "mock SOC response"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19},
		})
		return http.StatusOK, body, ct
	}
	body, _ := json.Marshal(map[string]any{
		"id": "mock", "type": "message", "role": "assistant", "model": model,
		"content":     []map[string]any{{"type": "text", "text": "mock SOC response"}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 12, "output_tokens": 7},
	})
	return http.StatusOK, body, ct
}
