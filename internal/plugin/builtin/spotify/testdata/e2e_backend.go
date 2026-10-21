package spotify

import (
	"context"
	"os"
	"strings"
	"sync"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/sdk"
)

// e2eMu keeps the scripts of concurrent reads and commands apart.
var e2eMu sync.Mutex

// e2eRunner answers a playing track and records every script, so the real
// app runs without osascript or Spotify.
type e2eRunner struct{}

func (e2eRunner) Run(_ context.Context, script string) (string, error) {
	e2eMu.Lock()
	if path := os.Getenv("KERO_SPOTIFY_E2E_SCRIPTS"); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = file.WriteString(script + "\n---\n")
			_ = file.Close()
		}
	}
	e2eMu.Unlock()
	return strings.Join([]string{
		"playing", "Test Song", "Test Artist", "Test Album",
		"https://i.scdn.co/image/test", "200", "50", "70", "false", "false",
	}, "\x1f") + "\n", nil
}

// e2eSource listens to nothing.
type e2eSource struct{}

func (e2eSource) Start(func()) error { return nil }
func (e2eSource) Close() error       { return nil }

// e2eClosedURL is a loopback port nothing listens on: a library call of a
// test that set no server fails at once, and never reaches Spotify.
const e2eClosedURL = "http://127.0.0.1:1"

// newBackend is the player, the listener and a signed-in library of the
// built app: no osascript, no real credentials, and a Web API that is the
// test's own server, named in KERO_SPOTIFY_E2E_API.
var newBackend = func(*sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
	api := os.Getenv("KERO_SPOTIFY_E2E_API")
	if api == "" {
		api = e2eClosedURL
	}
	client := web.New(web.Config{
		BaseURL:     api,
		AccountsURL: api,
		OpenURL:     web.OpenURLFunc(func(string) error { return nil }),
		Secrets:     web.MapSecrets{web.TokensKey: "{\"access_token\":\"test\",\"token_type\":\"Bearer\",\"expires_at\":\"2099-01-01T00:00:00Z\"}"},
	})
	client.SetClientID("e2e-client")
	return transport.New(e2eRunner{}), e2eSource{}, client
}
