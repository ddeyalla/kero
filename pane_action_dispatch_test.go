package main

import (
	"encoding/json"
	"testing"
	"time"

	"kero/internal/proto"
)

func TestNewAttentionActionCreatesAttentionPane(t *testing.T) {
	a := testApp(t)
	requests := attentionRPC(t, a)
	a.run("pane.new.attention", nil)
	select {
	case req := <-requests:
		var params proto.CreateParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatal(err)
		}
		if req.Method != "split" || params.View != proto.PaneAttention {
			t.Fatalf("action sent %s %+v", req.Method, params)
		}
	case <-time.After(time.Second):
		t.Fatal("New Attention did not create a pane")
	}
}
