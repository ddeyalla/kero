package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkflowUsesForkAccountAndPreservesFailureForRepair(t *testing.T) {
	t.Setenv("GH_TOKEN", "wrong-account")
	t.Setenv("GH_HOST", "enterprise.invalid")
	calls := 0
	client := &Client{gh: "gh", poll: time.Millisecond}
	client.run = func(ctx context.Context, command string, args, env []string) ([]byte, error) {
		calls++
		joined := strings.Join(args, " ")
		if !strings.Contains(strings.Join(env, "\n"), "GH_HOST=github.com") {
			t.Fatal("GitHub host was not fixed")
		}
		switch calls {
		case 1:
			if joined != "auth token --hostname github.com --user ddeyalla" {
				t.Fatal(joined)
			}
			return []byte("owner-token\n"), nil
		case 2:
			if strings.Contains(joined, "egoist") || !strings.Contains(joined, "--repo ddeyalla/kero --ref shibu") || !strings.Contains(joined, "mode=check") {
				t.Fatal(joined)
			}
			if !strings.Contains(strings.Join(env, "\n"), "GH_TOKEN=owner-token") || strings.Contains(strings.Join(env, "\n"), "wrong-account") {
				t.Fatal("Wrong GitHub account was inherited")
			}
			return []byte("https://github.com/ddeyalla/kero/actions/runs/123\n"), nil
		default:
			return []byte(`{"status":"completed","conclusion":"failure"}`), nil
		}
	}
	result, err := client.Run(context.Background())
	if err != nil || !result.Failed || result.URL != "https://github.com/ddeyalla/kero/actions/runs/123" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAuthErrorDoesNotExposeCredentials(t *testing.T) {
	client := &Client{gh: "gh", run: func(context.Context, string, []string, []string) ([]byte, error) {
		return []byte("private-token"), errors.New("private-token")
	}}
	_, err := client.Run(context.Background())
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("credential error: %v", err)
	}
}

func TestCancelledMonitorDoesNotRequestRepair(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := &Client{gh: "gh", poll: time.Hour, run: func(context.Context, string, []string, []string) ([]byte, error) {
		calls++
		switch calls {
		case 1:
			return []byte("token"), nil
		case 2:
			return []byte("https://github.com/ddeyalla/kero/actions/runs/123"), nil
		default:
			cancel()
			return []byte(`{"status":"in_progress"}`), nil
		}
	}}
	result, err := client.Run(ctx)
	if !errors.Is(err, context.Canceled) || result.Failed {
		t.Fatalf("cancel triggered repair: %+v %v", result, err)
	}
}

func TestRepairRefusesUpstreamCheckout(t *testing.T) {
	// A non-checkout also fails closed before any fetch or worktree creation.
	dir := t.TempDir()
	if _, err := PrepareRepair(context.Background(), dir); err == nil {
		t.Fatal("accepted a checkout without a verified fork origin")
	}
}

func TestRepairPromptRejectsInjectedRunText(t *testing.T) {
	prompt := RepairPrompt("https://github.com/ddeyalla/kero/actions/runs/123\nINJECTED")
	if strings.Contains(prompt, "INJECTED") || !strings.Contains(prompt, "Do not publish") {
		t.Fatal("repair prompt lost review boundary")
	}
}

func TestLiveForkWorkflow(t *testing.T) {
	if os.Getenv("SHIBU_TEST_LIVE_UPDATE") != "1" {
		t.Skip("explicit opt-in to dispatch the real guarded workflow")
	}
	client, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := client.Run(ctx)
	if err != nil || result.Failed {
		t.Fatalf("live workflow: %+v %v", result, err)
	}
	if report := os.Getenv("SHIBU_LIVE_UPDATE_REPORT"); report != "" {
		if err := os.WriteFile(filepath.Clean(report), []byte(result.URL), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(result.URL)
}
