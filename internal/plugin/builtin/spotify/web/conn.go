package web

import "kero/internal/plugin/sdk"

// ConnOpenURL returns an OpenURL that opens a link through the plugin's
// connection to its host.
func ConnOpenURL(conn *sdk.Conn) OpenURL { return connOpenURL{conn} }

type connOpenURL struct{ conn *sdk.Conn }

func (o connOpenURL) OpenURL(url string) error { return o.conn.OpenURL(url) }

// ConnSecrets returns a Secrets that reads and writes the plugin's own
// secrets through its connection to the host.
func ConnSecrets(conn *sdk.Conn) Secrets { return connSecrets{conn} }

type connSecrets struct{ conn *sdk.Conn }

func (s connSecrets) Get(key string) (string, error) { return s.conn.Secret(key) }

func (s connSecrets) Set(key, value string) error { return s.conn.SetSecret(key, value) }
