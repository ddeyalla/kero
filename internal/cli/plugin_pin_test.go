package cli

import (
	"errors"
	"testing"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
)

// kero plugin pin checks the plugin and the widget against the ones the app
// knows, and unpin takes any pin off.
func TestPinAction(t *testing.T) {
	plugins := []registry.Plugin{{Manifest: protocol.Manifest{
		ID: "linear", Name: "Linear", Version: "1", Run: []string{"x"},
		Widgets: []protocol.Widget{{Name: "board", Title: "Board"}},
	}}}
	action, kv, err := pinAction("pin", []string{"linear"}, plugins)
	if err != nil || action != "plugin.pin" || kv["plugin"] != "linear" || kv["widget"] != "" {
		t.Fatalf("pin linear = %q %v %v", action, kv, err)
	}
	if _, kv, err = pinAction("pin", []string{"linear", "board"}, plugins); err != nil || kv["widget"] != "board" {
		t.Fatalf("pin linear board = %v %v", kv, err)
	}
	for _, args := range [][]string{{"nope"}, {"linear", "nope"}} {
		if _, _, err := pinAction("pin", args, plugins); !errors.Is(err, errNotFound) {
			t.Errorf("pin %v = %v, want a not-found error", args, err)
		}
	}
	if action, kv, err = pinAction("unpin", []string{"gone"}, plugins); err != nil || action != "plugin.unpin" || kv["plugin"] != "gone" {
		t.Fatalf("unpin gone = %q %v %v", action, kv, err)
	}
	if err := pluginCmd([]string{"pin"}); !errors.Is(err, errUsage) {
		t.Errorf("pin with no plugin = %v, want a usage error", err)
	}
	if err := pluginCmd([]string{"toggle", "linear"}); !errors.Is(err, errUsage) {
		t.Errorf("toggle = %v, want a usage error", err)
	}
}
