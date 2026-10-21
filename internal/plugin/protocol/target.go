package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrTarget is what a plugin pane's target reads as when it does not
// parse.
var ErrTarget = errors.New("plugin target")

// Target is what a plugin pane keeps in proto.Pane.Target: the plugin,
// the widget, and the state the plugin saved, so that the pane comes
// back after a restart.
type Target struct {
	Plugin string          `json:"plugin"`
	Widget string          `json:"widget,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
}

// EncodeTarget writes a plugin pane's target.
func EncodeTarget(t Target) (string, error) {
	if !idRe.MatchString(t.Plugin) {
		return "", fmt.Errorf("%w: plugin %q", ErrTarget, t.Plugin)
	}
	if len(t.State) > 0 && !json.Valid(t.State) {
		return "", fmt.Errorf("%w: state is not JSON", ErrTarget)
	}
	b, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTarget, err)
	}
	return string(b), nil
}

// DecodeTarget reads a plugin pane's target. A pane of the plugin view
// without one is an error: it names no plugin to draw.
func DecodeTarget(s string) (Target, error) {
	var t Target
	if len(s) == 0 {
		return t, fmt.Errorf("%w: empty", ErrTarget)
	}
	if err := json.Unmarshal([]byte(s), &t); err != nil {
		return t, fmt.Errorf("%w: %v", ErrTarget, err)
	}
	if !idRe.MatchString(t.Plugin) {
		return t, fmt.Errorf("%w: plugin %q", ErrTarget, t.Plugin)
	}
	return t, nil
}
