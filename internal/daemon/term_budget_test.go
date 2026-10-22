package daemon

import (
	"path/filepath"
	"testing"

	"kero/internal/settings"
)

// kerod's terminals keep the image budget, as the app's do: MyGo's default
// of 320 MB for every pane's images is not what 25 panes can pay.
func TestPaneTerminalImageLimit(t *testing.T) {
	t.Setenv("KERO_CONFIG", filepath.Join(t.TempDir(), "config"))
	s := &pane{}
	if got := s.termOptions().ImageLimit; got != settings.ImageLimitBytes {
		t.Errorf("the pane's terminal keeps %d bytes of images, want %d", got, settings.ImageLimitBytes)
	}
}
