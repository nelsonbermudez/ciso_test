// Command transect: exports the OpenSearch events to OpenClaw
// telemetry-hal format (.jsonl, one file per session) so that Transect
// (https://github.com/AI-Safety-Institute/transect) can analyze them.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"aisoc/internal/config"
	"aisoc/internal/dlp"
	"aisoc/internal/events"
	"aisoc/internal/openclaw"
	"aisoc/internal/store"
)

func main() {
	osURLs := config.List("OPENSEARCH_URLS")
	index := config.Get("OPENSEARCH_INDEX", "soc-events")
	outDir := config.Get("EXPORT_DIR", "output_files")

	interval := 3600 * time.Second
	if v := os.Getenv("EXPORT_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}

	client, err := store.NewClient(osURLs)
	if err != nil {
		log.Fatalf("opensearch: %v", err)
	}
	ctx := context.Background()
	if err := store.WaitReady(ctx, client, 90*time.Second); err != nil {
		log.Fatalf("opensearch unavailable: %v", err)
	}

	query := []byte(`{"query":{"match_all":{}}}`)
	log.Printf("transect: exporting index %q to %q every %s (OpenClaw telemetry-hal)", index, outDir, interval)

	for {
		docs, err := store.ScrollAll(ctx, client, index, 1000, query)
		if err != nil {
			log.Printf("export error: %v", err)
		} else if err := exportOpenClaw(outDir, docs); err != nil {
			log.Printf("export error: %v", err)
		}
		time.Sleep(interval)
	}
}

// exportOpenClaw groups the events by session and writes one OpenClaw .jsonl
// per session in outDir.
func exportOpenClaw(outDir string, docs [][]byte) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	var evs []events.Event
	for _, d := range docs {
		var e events.Event
		if json.Unmarshal(d, &e) == nil {
			evs = append(evs, e)
		}
	}
	bySession := map[string][]events.Event{}
	for _, e := range evs {
		sid := e.SessionID
		if sid == "" {
			sid = e.Source
		}
		if sid == "" {
			sid = "unknown"
		}
		bySession[sid] = append(bySession[sid], e)
	}
	n := 0
	for sid, sess := range bySession {
		if err := writeSession(outDir, sid, sess); err != nil {
			return err
		}
		n++
	}
	log.Printf("exported %d sessions (%d events)", n, len(evs))
	return nil
}

func writeSession(outDir, sid string, sess []events.Event) error {
	sort.SliceStable(sess, func(i, j int) bool {
		ti, tj := parseTs(sess[i].Timestamp), parseTs(sess[j].Timestamp)
		if ti.Equal(tj) {
			return sess[i].RequestID < sess[j].RequestID
		}
		return ti.Before(tj)
	})

	var msgs []openclaw.Message
	for _, e := range sess {
		switch e.Type {
		case "request":
			msgs = append(msgs, openclaw.Message{
				"role":           "user",
				"idempotencyKey": e.RequestID,
				"timestamp":      parseTs(e.Timestamp).UnixMilli(),
				"content":        e.Content,
			})
		case "response":
			msgs = append(msgs, assistantMsg(e, "stop"))
		case "block":
			msgs = append(msgs, assistantMsg(e, "error"))
		}
	}
	path, err := openclaw.WriteSession(outDir, sid, msgs)
	if err != nil {
		return err
	}
	if path != "" {
		log.Printf("exported %s (%d turns)", path, len(msgs))
	}
	return nil
}

func assistantMsg(e events.Event, stopReason string) map[string]any {
	api, provider := "openai-chat-completions", "deepseek"
	if e.Protocol == "anthropic" {
		api, provider = "anthropic-messages", "anthropic"
	}
	model := e.Model
	if model == "" {
		model = "unknown-model"
	}
	content := e.Content
	if e.Type == "block" {
		content = "[blocked by the SOC: " + strings.Join(ruleNames(e.Findings), ", ") + "]"
	}
	return map[string]any{
		"role":          "assistant",
		"responseId":    e.RequestID,
		"timestamp":     parseTs(e.Timestamp).UnixMilli(),
		"api":           api,
		"provider":      provider,
		"model":         model,
		"responseModel": model,
		"stopReason":    stopReason,
		"content":       content,
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

func parseTs(ts string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}
