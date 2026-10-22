package spotify

import (
	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/sdk"
)

// newBackend builds the player, the playback listener, and the Web API
// client of one plugin process. A test replaces it with fakes. The client
// keeps web.DefaultRedirectPort, so its redirect URI is the one the
// manifest's help and the Set Up state tell the user to register.
var newBackend = func(conn *sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
	client := web.New(web.Config{
		OpenURL: web.ConnOpenURL(conn),
		Secrets: web.ConnSecrets(conn),
	})
	return transport.New(transport.OSAscript{}), transport.NewSource(), client
}

// newEngine makes the engine of one plugin process: its page runs in the
// host's web view, and it asks the Web API client for the access token.
var newEngine = func(conn *sdk.Conn, library *web.Client, emit func(transport.Snapshot), onError func(error)) playerEngine {
	// A plugin that has no connection to its host has no web view to run.
	if conn == nil {
		return nil
	}
	return engine.New(engine.Config{
		Web:     conn,
		Token:   library.AccessToken,
		Emit:    emit,
		OnError: onError,
	})
}
