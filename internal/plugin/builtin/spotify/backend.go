package spotify

import (
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
