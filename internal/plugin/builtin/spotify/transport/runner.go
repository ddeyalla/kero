package transport

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// osascriptPath is where macOS keeps osascript.
const osascriptPath = "/usr/bin/osascript"

// Runner runs one AppleScript. The plugin uses [OSAscript] and tests use
// a fake, so a test never runs a real AppleScript or touches Spotify.
type Runner interface {
	// Run runs the script and returns its standard output. A script
	// that fails returns an error that says why, in a [CommandError]
	// when osascript wrote the reason.
	Run(ctx context.Context, script string) (string, error)
}

// OSAscript is the Runner the plugin uses. It runs each script with
// /usr/bin/osascript in a process of its own.
type OSAscript struct{}

// Run runs one AppleScript and returns its standard output.
func (OSAscript) Run(ctx context.Context, script string) (string, error) {
	command := exec.CommandContext(ctx, osascriptPath, "-e", script)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", &CommandError{Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.String(), nil
}

// CommandError is an osascript failure and what osascript wrote to
// standard error, where it puts the reason: a refused Apple event, an
// app that stopped running, or a script that did not compile.
type CommandError struct {
	Stderr string
	Err    error
}

// Error describes the failure with osascript's own words when it gave
// any.
func (e *CommandError) Error() string {
	if e.Stderr == "" {
		return "osascript: " + e.Err.Error()
	}
	return "osascript: " + e.Stderr
}

// Unwrap returns the error osascript exited with, so that a caller can
// tell a timeout or a missing osascript from a script failure.
func (e *CommandError) Unwrap() error {
	return e.Err
}

// compile-time check that the plugin's runner satisfies Runner.
var _ Runner = OSAscript{}
