package cost

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"time"
)

type claudeLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			In            int64 `json:"input_tokens"`
			Out           int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			Creation      struct {
				H1 int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

// parseClaude reads Claude Code transcript lines. The line's own costUSD is
// ignored on purpose, so every provider is priced from tokens by one table.
func parseClaude(ctx context.Context, r io.ReadSeeker, off int64, loc *time.Location) (int64, []event) {
	var events []event
	off = scanLines(ctx, r, off, func(line []byte) {
		if !hasAll(line, hintUsage, hintAsst) {
			return
		}
		var l claudeLine
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message.Usage == nil {
			return
		}
		m, u := l.Message.Model, l.Message.Usage
		if m == "" || m == "<synthetic>" {
			return
		}
		ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			return
		}
		b := Bucket{In: u.In, Out: u.Out, CR: u.CacheRead}
		if h1 := u.Creation.H1; h1 > 0 && h1 <= u.CacheCreation {
			b.CW1, b.CW5 = h1, u.CacheCreation-h1
		} else {
			b.CW5 = u.CacheCreation
		}
		if b.tokens() == 0 {
			return
		}
		ev := event{day: dayKey(ts, loc), provider: providerOf(m, "claude"), model: m, b: b}
		if l.Message.ID != "" || l.RequestID != "" {
			h := fnv.New64a()
			h.Write([]byte(l.Message.ID))
			h.Write([]byte{0})
			h.Write([]byte(l.RequestID))
			if ev.key = h.Sum64(); ev.key == 0 {
				ev.key = 1
			}
		}
		events = append(events, ev)
	})
	return off, events
}
