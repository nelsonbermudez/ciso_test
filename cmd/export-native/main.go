// Command export-native: exports native transcripts to OpenClaw
// telemetry-hal format for Transect. Sources: Hermes (state.db) and Claude Code
// (~/.claude/projects/*.jsonl). One-shot: exports and exits.
package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"aisoc/internal/config"
	"aisoc/internal/openclaw"
)

func main() {
	outDir := config.Get("EXPORT_DIR", "output_files/openclaw-native")
	source := strings.ToLower(config.Get("SOURCE", "all"))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	if source == "hermes" || source == "all" {
		dbPath := expand(config.Get("HERMES_DB", "~/.hermes/state.db"))
		if err := exportHermes(dbPath, outDir); err != nil {
			log.Printf("hermes: %v", err)
		}
	}
	if source == "claude" || source == "all" {
		dir := expand(config.Get("CLAUDE_DIR", "~/.claude/projects"))
		if err := exportClaude(dir, outDir); err != nil {
			log.Printf("claude: %v", err)
		}
	}
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// ---------- Hermes (state.db) ----------

type sessRow struct {
	id, model, provider string
}

func exportHermes(dbPath, outDir string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open state.db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, model, billing_provider FROM sessions ORDER BY rowid DESC")
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []sessRow
	for rows.Next() {
		var s sessRow
		var provider sql.NullString
		if err := rows.Scan(&s.id, &s.model, &provider); err != nil {
			return err
		}
		s.provider = provider.String
		sessions = append(sessions, s)
	}

	n := 0
	for _, s := range sessions {
		msgs, err := hermesMessages(db, s)
		if err != nil {
			return err
		}
		path, err := openclaw.WriteSession(outDir, s.id, msgs)
		if err != nil {
			return err
		}
		if path != "" {
			log.Printf("hermes: exported %s (%d turns)", path, len(msgs))
			n++
		}
	}
	log.Printf("hermes: %d sessions exported", n)
	return nil
}

func hermesMessages(db *sql.DB, s sessRow) ([]openclaw.Message, error) {
	provider := s.provider
	if provider == "" {
		provider = "deepseek"
	}
	model := s.model
	if model == "" {
		model = "unknown-model"
	}
	rows, err := db.Query(
		"SELECT id, role, content, tool_calls, tool_name, tool_call_id, finish_reason, timestamp FROM messages WHERE session_id = ? ORDER BY id ASC",
		s.id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []openclaw.Message
	for rows.Next() {
		var id int64
		var role, content, finish sql.NullString
		var toolCalls, toolName, toolCallID sql.NullString
		var ts sql.NullFloat64
		if err := rows.Scan(&id, &role, &content, &toolCalls, &toolName, &toolCallID, &finish, &ts); err != nil {
			return nil, err
		}
		tsMs := int64(0)
		if ts.Valid {
			tsMs = int64(ts.Float64 * 1000)
		}
		switch role.String {
		case "user":
			msgs = append(msgs, openclaw.Message{
				"role":           "user",
				"idempotencyKey": strconv.FormatInt(id, 10),
				"timestamp":      tsMs,
				"content":        content.String,
			})
		case "assistant":
			msgs = append(msgs, hermesAssistant(id, tsMs, provider, model, content.String, toolCalls.String, finish.String))
		case "tool":
			msgs = append(msgs, openclaw.Message{
				"role":       "toolResult",
				"toolCallId": toolCallID.String,
				"toolName":   toolName.String,
				"timestamp":  tsMs,
				"isError":    false,
				"content":    content.String,
			})
		}
	}
	return msgs, nil
}

func hermesAssistant(id int64, tsMs int64, provider, model, content, toolCallsJSON, finish string) openclaw.Message {
	stopReason := "stop"
	if finish == "tool_calls" {
		stopReason = "toolUse"
	}
	var msgContent any = content
	if toolCallsJSON != "" {
		var tcs []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if json.Unmarshal([]byte(toolCallsJSON), &tcs) == nil && len(tcs) > 0 {
			blocks := []map[string]any{}
			if content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": content})
			}
			for _, tc := range tcs {
				var args any = tc.Function.Arguments
				var parsed any
				if json.Unmarshal([]byte(tc.Function.Arguments), &parsed) == nil {
					args = parsed
				}
				blocks = append(blocks, map[string]any{
					"type":      "toolCall",
					"id":        tc.ID,
					"name":      tc.Function.Name,
					"arguments": args,
				})
			}
			msgContent = blocks
		}
	}
	return openclaw.Message{
		"role":          "assistant",
		"responseId":    strconv.FormatInt(id, 10),
		"timestamp":     tsMs,
		"api":           "openai-chat-completions",
		"provider":      provider,
		"model":         model,
		"responseModel": model,
		"stopReason":    stopReason,
		"content":       msgContent,
	}
}

// ---------- Claude Code (~/.claude/projects/*.jsonl) ----------
// Verified against a simulated transcript; not validated against real ~/.claude
// (Claude Code is not installed on this machine).

type claudeMessage struct {
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason"`
}

type claudeLine struct {
	Type      string        `json:"type"`
	SessionID string        `json:"sessionId"`
	UUID      string        `json:"uuid"`
	Timestamp string        `json:"timestamp"`
	Message   claudeMessage `json:"message"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func exportClaude(dir, outDir string) error {
	files, _ := filepath.Glob(filepath.Join(dir, "**", "*.jsonl"))
	if len(files) == 0 {
		log.Printf("claude: no transcripts in %s", dir)
		return nil
	}
	n := 0
	for _, f := range files {
		c, err := exportClaudeFile(f, outDir)
		if err != nil {
			log.Printf("claude %s: %v", f, err)
			continue
		}
		n += c
	}
	log.Printf("claude: %d sessions exported from %d files", n, len(files))
	return nil
}

func exportClaudeFile(path, outDir string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	bySession := map[string][]openclaw.Message{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var l claudeLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.SessionID == "" {
			continue
		}
		tsMs := claudeTs(l.Timestamp)
		switch l.Type {
		case "user":
			bySession[l.SessionID] = append(bySession[l.SessionID], claudeUserTurns(l.Message.Content, l.UUID, tsMs)...)
		case "assistant":
			bySession[l.SessionID] = append(bySession[l.SessionID], claudeAssistant(l.Message, l.UUID, tsMs))
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}

	n := 0
	for sid, msgs := range bySession {
		path, err := openclaw.WriteSession(outDir, sid, msgs)
		if err != nil {
			return 0, err
		}
		if path != "" {
			n++
		}
	}
	return n, nil
}

func claudeUserTurns(content json.RawMessage, idemKey string, tsMs int64) []openclaw.Message {
	blocks := parseClaudeBlocks(content)
	if len(blocks) == 0 {
		return nil
	}
	var out []openclaw.Message
	var textParts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "tool_result":
			out = append(out, openclaw.Message{
				"role":       "toolResult",
				"toolCallId": b.ToolUseID,
				"timestamp":  tsMs,
				"isError":    b.IsError,
				"content":    claudeContentString(b.Content),
			})
		}
	}
	if len(textParts) > 0 {
		out = append([]openclaw.Message{{
			"role":           "user",
			"idempotencyKey": idemKey,
			"timestamp":      tsMs,
			"content":        strings.Join(textParts, "\n"),
		}}, out...)
	}
	return out
}

func claudeAssistant(msg claudeMessage, responseID string, tsMs int64) openclaw.Message {
	model := msg.Model
	if model == "" {
		model = "unknown-model"
	}
	blocks := parseClaudeBlocks(msg.Content)
	var content []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": b.Text})
			}
		case "tool_use":
			content = append(content, map[string]any{"type": "toolCall", "id": b.ID, "name": b.Name, "arguments": b.Input})
		}
	}
	var out any = content
	if len(content) == 1 && content[0]["type"] == "text" {
		out = content[0]["text"]
	}
	return openclaw.Message{
		"role":          "assistant",
		"responseId":    responseID,
		"timestamp":     tsMs,
		"api":           "anthropic-messages",
		"provider":      "anthropic",
		"model":         model,
		"responseModel": model,
		"stopReason":    claudeStopReason(msg.StopReason),
		"content":       out,
	}
}

func parseClaudeBlocks(raw json.RawMessage) []claudeBlock {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []claudeBlock{{Type: "text", Text: s}}
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return blocks
	}
	return nil
}

func claudeTs(ts string) int64 {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.UnixMilli()
	}
	return 0
}

func claudeContentString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func claudeStopReason(s string) string {
	if s == "tool_use" {
		return "toolUse"
	}
	return "stop"
}
