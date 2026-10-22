package cost

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

type codexUsage struct {
	In     int64 `json:"input_tokens"`
	Cached int64 `json:"cached_input_tokens"`
	Out    int64 `json:"output_tokens"`
}

type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Info  *struct {
			Total *codexUsage `json:"total_token_usage"`
			Last  *codexUsage `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// parseCodex reads Codex rollout lines. Totals only grow within a session, so
// each token_count event adds the difference from the one before it. The model
// and the previous totals travel in the cursor, because they span appends.
func parseCodex(ctx context.Context, r io.ReadSeeker, off int64, model string, prev [3]int64, loc *time.Location) (int64, string, [3]int64, []event) {
	var events []event
	off = scanLines(ctx, r, off, func(line []byte) {
		isCtx := hasAll(line, hintCtx)
		if !isCtx && !hasAll(line, hintTokenCnt) {
			return
		}
		var l codexLine
		if json.Unmarshal(line, &l) != nil {
			return
		}
		if l.Type == "turn_context" {
			if l.Payload.Model != "" {
				model = l.Payload.Model
			}
			return
		}
		if l.Payload.Type != "token_count" || l.Payload.Info == nil {
			return
		}
		var d codexUsage
		if t := l.Payload.Info.Total; t != nil {
			cur := [3]int64{t.In, t.Cached, t.Out}
			if cur == prev {
				return
			}
			d = codexUsage{cur[0] - prev[0], cur[1] - prev[1], cur[2] - prev[2]}
			if d.In < 0 || d.Cached < 0 || d.Out < 0 {
				// The counter restarted.
				d = *t
			}
			prev = cur
		} else if t := l.Payload.Info.Last; t != nil {
			d = *t
		} else {
			return
		}
		ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			return
		}
		// Codex input counts cached tokens too; the output counts reasoning.
		b := Bucket{In: max(d.In-d.Cached, 0), CR: d.Cached, Out: d.Out}
		if b.tokens() == 0 {
			return
		}
		m := model
		if m == "" {
			m = "unknown"
		}
		events = append(events, event{day: dayKey(ts, loc), provider: "codex", model: m, b: b})
	})
	return off, model, prev, events
}
