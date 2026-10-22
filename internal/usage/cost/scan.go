package cost

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxLine is the longest line that is parsed. A longer line (for example one
// that holds an image) is skipped, because usage lines are small.
const maxLine = 16 << 20

// scanLines calls fn for each complete line from offset off, and returns the
// offset after the last complete line. A partial last line is left for the
// next call, because the agent may still be writing it. fn must not keep line.
// When ctx ends it stops early and returns the offset after the last line done.
func scanLines(ctx context.Context, r io.ReadSeeker, off int64, fn func(line []byte)) int64 {
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		return off
	}
	br := bufio.NewReaderSize(r, 256<<10)
	var long []byte
	skipping := false
	pos, committed := off, off
	for lines := 0; ; lines++ {
		if lines%512 == 0 && ctx.Err() != nil {
			return committed
		}
		chunk, err := br.ReadSlice('\n')
		pos += int64(len(chunk))
		switch {
		case err == nil:
			switch {
			case skipping:
				skipping = false
			case long != nil:
				long = append(long, chunk...)
				fn(long)
			default:
				fn(chunk)
			}
			long = nil
			committed = pos
		case errors.Is(err, bufio.ErrBufferFull):
			if skipping {
				continue
			}
			long = append(long, chunk...)
			if len(long) > maxLine {
				long, skipping = nil, true
			}
		default:
			return committed
		}
	}
}

func claudeRoots() []string {
	var roots []string
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		for _, p := range strings.Split(env, ",") {
			if p = strings.TrimSpace(p); p != "" {
				roots = append(roots, filepath.Join(p, "projects"))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".config", "claude", "projects"), filepath.Join(home, ".claude", "projects"))
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range roots {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func codexRoot() string {
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return filepath.Join(env, "sessions")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".codex", "sessions")
	}
	return ""
}

var (
	hintUsage    = []byte(`"usage"`)
	hintAsst     = []byte(`"assistant"`)
	hintCtx      = []byte(`"turn_context"`)
	hintTokenCnt = []byte(`"token_count"`)
)

func hasAll(line []byte, hints ...[]byte) bool {
	for _, h := range hints {
		if !bytes.Contains(line, h) {
			return false
		}
	}
	return true
}
