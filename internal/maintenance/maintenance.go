// Package maintenance starts the fork's guarded workflow. It never installs
// code, restarts sessions, or publishes an agent's repairs.
package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const WorkflowURL = "https://github.com/ddeyalla/kero/actions/workflows/shibu-update.yml"

type Result struct {
	URL    string
	Failed bool
}

type Client struct {
	gh   string
	run  func(context.Context, string, []string, []string) ([]byte, error)
	poll time.Duration
}

func New() (*Client, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		for _, path := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
			if info, e := os.Stat(path); e == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				gh, err = path, nil
				break
			}
		}
	}
	if err != nil {
		return nil, errors.New("Install GitHub CLI (gh) and sign in as ddeyalla to start source updates")
	}
	return &Client{gh: gh, poll: 10 * time.Second, run: func(ctx context.Context, command string, args, env []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Env = env
		return cmd.Output()
	}}, nil
}

var runLink = regexp.MustCompile(`https://github\.com/ddeyalla/kero/actions/runs/([0-9]+)`)

// Run uses the fork owner's existing gh login, held only in memory. A token
// inherited from another account must never choose the update destination.
func (c *Client) Run(ctx context.Context) (Result, error) {
	env := append(withoutTokens(os.Environ()), "GH_HOST=github.com")
	auth, err := c.run(ctx, c.gh, []string{"auth", "token", "--hostname", "github.com", "--user", "ddeyalla"}, env)
	if err != nil || strings.TrimSpace(string(auth)) == "" {
		return Result{}, errors.New("Sign in to GitHub CLI as ddeyalla, then retry the update")
	}
	env = append(env, "GH_TOKEN="+strings.TrimSpace(string(auth)))
	started, err := c.run(ctx, c.gh, []string{"workflow", "run", "shibu-update.yml", "--repo", "ddeyalla/kero", "--ref", "shibu", "-f", "mode=check", "-F", "publish=true"}, env)
	if err != nil {
		return Result{}, errors.New("Could not start the Shibu update workflow; check GitHub CLI access to ddeyalla/kero")
	}
	link := runLink.FindStringSubmatch(string(started))
	if len(link) != 2 {
		return Result{URL: WorkflowURL}, errors.New("Update requested; this gh version cannot track it. View progress in GitHub Actions")
	}
	result := Result{URL: link[0]}
	for {
		data, err := c.run(ctx, c.gh, []string{"run", "view", link[1], "--repo", "ddeyalla/kero", "--json", "status,conclusion"}, env)
		if err != nil {
			return result, errors.New("Could not read update progress; the workflow may still be running in GitHub Actions")
		}
		var state struct{ Status, Conclusion string }
		if json.Unmarshal(data, &state) != nil {
			return result, errors.New("Invalid update progress response")
		}
		if state.Status == "completed" {
			switch state.Conclusion {
			case "success":
				return result, nil
			case "failure":
				result.Failed = true
				return result, nil
			default:
				return result, fmt.Errorf("Update stopped (%s); current app remains installed", state.Conclusion)
			}
		}
		timer := time.NewTimer(c.poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}

func withoutTokens(env []string) []string {
	var clean []string
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key != "GH_TOKEN" && key != "GITHUB_TOKEN" && key != "GH_ENTERPRISE_TOKEN" && key != "GITHUB_ENTERPRISE_TOKEN" && key != "GH_HOST" {
			clean = append(clean, entry)
		}
	}
	return clean
}

// PrepareRepair creates a separate branch/worktree; the user's working tree
// and its uncommitted edits are not changed. Fetch is read-only from the fork.
func PrepareRepair(ctx context.Context, repo string) (string, error) {
	git := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).Output()
	}
	origin, err := git("remote", "get-url", "origin")
	url := strings.TrimSuffix(strings.TrimSpace(string(origin)), ".git")
	if err != nil || (url != "https://github.com/ddeyalla/kero" && url != "git@github.com:ddeyalla/kero" && url != "ssh://git@github.com/ddeyalla/kero") {
		return "", errors.New("Repair requires a checkout whose origin is ddeyalla/kero")
	}
	if _, err := git("fetch", "origin", "shibu"); err != nil {
		return "", errors.New("Could not fetch the Shibu branch for repair")
	}
	temp, err := os.MkdirTemp("", "shibu-update-repair-")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(temp, "source")
	branch := "repair/" + filepath.Base(temp)
	if _, err := git("worktree", "add", "-b", branch, dir, "origin/shibu"); err != nil {
		_ = os.RemoveAll(temp)
		return "", errors.New("Could not create an isolated repair checkout")
	}
	return dir, nil
}

func RepairPrompt(runURL string) string {
	if runLink.FindString(runURL) != runURL || runURL == "" {
		runURL = WorkflowURL
	}
	return `Diagnose the failed Shibu Code update: ` + runURL + `
Work only in this isolated repair checkout. Read AGENTS.md and docs/automatic-updates.md.
The only writable GitHub destination is ddeyalla/kero. egoist/kero is read/fetch only.
Inspect the failed workflow, merge the latest stable Kero release if needed, and resolve conflicts while preserving Shibu's app/data identity, icon, usage features, signed update feed, and existing session compatibility. Do not skip tests or disable safety guards to make a build pass.
Run release-guard tests, the application tests and MyGo vet. If migration or protocol compatibility is unclear, stop and explain it.
Do not publish a release, merge to shibu, install an app, restart the GUI/daemon/shells, or modify the user's active working tree. Prepare a repair commit on this branch and summarize the diff, tests and remaining risks for human review. Do not push to egoist/kero or open an upstream pull request.`
}
