// Package web is the Spotify Web API side of the Spotify plugin: the
// sign-in with PKCE, the tokens, and the calls for search, playlists and
// saved albums.
//
// The package has no UI. It gives the widget the data and a state for
// every way a call can fail: no Client ID, not signed in, a refused
// token, a lapsed Premium, and a network that is down. State reads that
// state from an error. When the network is down, a call returns the last
// results it got, so that the pane keeps them and adds a note.
package web

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// OpenURL opens a link in the user's browser.
//
// The app implements it through the plugin's openURL call, with
// ConnOpenURL. The tests use OpenURLFunc.
type OpenURL interface {
	OpenURL(url string) error
}

// Secrets keeps the plugin's own secrets.
//
// The app implements it through the plugin's secret calls, with
// ConnSecrets. The tests use MapSecrets.
type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

// MapSecrets is a Secrets in memory, which the tests use.
type MapSecrets map[string]string

// Get reads a secret, "" when it is not set.
func (m MapSecrets) Get(key string) (string, error) { return m[key], nil }

// Set keeps a secret. An empty value removes it, as the host does.
func (m MapSecrets) Set(key, value string) error {
	if value == "" {
		delete(m, key)
		return nil
	}
	m[key] = value
	return nil
}

// OpenURLFunc is an OpenURL made of a function, which the tests use.
type OpenURLFunc func(url string) error

// OpenURL calls the function.
func (f OpenURLFunc) OpenURL(url string) error { return f(url) }

// The Spotify endpoints the client uses when Config leaves them empty.
const (
	APIBaseURL      = "https://api.spotify.com"
	AccountsBaseURL = "https://accounts.spotify.com"
)

// TokensKey is the plugin secret that keeps the tokens.
const TokensKey = "tokens"

// SettingClientID is the key of the plugin setting that holds the Client
// ID of the user's Spotify developer app.
const SettingClientID = "clientId"

// DefaultScopes are the scopes the plugin asks for when Config.Scopes is
// empty: private playlists, saved albums and the playback state.
var DefaultScopes = []string{
	"playlist-read-private",
	"user-library-read",
	"user-read-playback-state",
}

// Config is what a Client needs.
type Config struct {
	// ClientID is the Client ID of the user's Spotify developer app.
	ClientID string
	// BaseURL is the Web API root. Empty is APIBaseURL.
	BaseURL string
	// AccountsURL is the accounts root. Empty is AccountsBaseURL.
	AccountsURL string
	// Scopes are the scopes to ask for. Empty is DefaultScopes.
	Scopes []string
	// RedirectPort is the port of the loopback callback. Zero picks a
	// free port. Set it when the Spotify dashboard holds a fixed URI.
	RedirectPort int
	// OpenURL opens the sign-in page in the browser.
	OpenURL OpenURL
	// Secrets keeps the tokens. Nil keeps them in memory only.
	Secrets Secrets
	// HTTP is the HTTP client. Nil is a client with a 30 s timeout.
	HTTP *http.Client
	// Now is the clock, for tests. Nil is time.Now.
	Now func() time.Time
	// SignInTimeout limits the wait for the callback. Zero is 5 minutes.
	SignInTimeout time.Duration
	// Listen opens the loopback listener, for tests. Nil is net.Listen.
	Listen func(network, address string) (net.Listener, error)
}

// Client is the Web API client of one plugin process. It is safe for
// concurrent use: the panes of one plugin share it.
type Client struct {
	baseURL       string
	accountsURL   string
	scopes        []string
	redirectPort  int
	openURL       OpenURL
	secrets       Secrets
	http          *http.Client
	now           func() time.Time
	signInTimeout time.Duration
	listen        func(network, address string) (net.Listener, error)

	clientMu sync.RWMutex
	clientID string

	tokenMu     sync.Mutex
	tokens      *Token
	tokenLoaded bool

	signInMu sync.Mutex

	cacheMu          sync.Mutex
	lastSearch       SearchResult
	lastSearchSet    bool
	lastPlaylists    []Playlist
	lastPlaylistsSet bool
	lastAlbums       []Album
	lastAlbumsSet    bool
}

// New returns a client for one plugin process.
//
// A nil Secrets keeps the tokens in memory, which is useful in a test
// but loses them when the process ends. The plugin itself passes a
// Secrets on its connection to the host.
func New(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = APIBaseURL
	}
	accounts := strings.TrimRight(cfg.AccountsURL, "/")
	if accounts == "" {
		accounts = AccountsBaseURL
	}
	scopes := slices.Clone(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = slices.Clone(DefaultScopes)
	}
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	timeout := cfg.SignInTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	listen := cfg.Listen
	if listen == nil {
		listen = net.Listen
	}
	secrets := cfg.Secrets
	if secrets == nil {
		secrets = MapSecrets{}
	}
	return &Client{
		baseURL:       base,
		accountsURL:   accounts,
		scopes:        scopes,
		redirectPort:  cfg.RedirectPort,
		openURL:       cfg.OpenURL,
		secrets:       secrets,
		http:          httpClient,
		now:           now,
		signInTimeout: timeout,
		listen:        listen,
		clientID:      strings.TrimSpace(cfg.ClientID),
	}
}

// ClientID returns the Client ID of the user's developer app, "" when
// the user set none.
func (c *Client) ClientID() string {
	c.clientMu.RLock()
	defer c.clientMu.RUnlock()
	return c.clientID
}

// SetClientID changes the Client ID, as the settings do.
func (c *Client) SetClientID(id string) {
	c.clientMu.Lock()
	c.clientID = strings.TrimSpace(id)
	c.clientMu.Unlock()
}

// SignedIn tells whether the client holds tokens.
func (c *Client) SignedIn() bool {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	t, err := c.loadTokensLocked()
	if err != nil {
		return false
	}
	return t != nil && (t.AccessToken != "" || t.RefreshToken != "")
}

// Current is the state the plugin shows before a call: no Client ID,
// not signed in, or ready.
func (c *Client) Current() string {
	if c.ClientID() == "" {
		return StateNoClientID
	}
	if !c.SignedIn() {
		return StateSignedOut
	}
	return StateReady
}

// SignOut forgets the tokens and the last results.
func (c *Client) SignOut() error {
	c.tokenMu.Lock()
	c.tokens = nil
	c.tokenLoaded = true
	c.tokenMu.Unlock()

	c.cacheMu.Lock()
	c.lastSearch = SearchResult{}
	c.lastSearchSet = false
	c.lastPlaylists = nil
	c.lastPlaylistsSet = false
	c.lastAlbums = nil
	c.lastAlbumsSet = false
	c.cacheMu.Unlock()

	if err := c.secrets.Set(TokensKey, ""); err != nil {
		return fmt.Errorf("spotify: forget the tokens: %w", err)
	}
	return nil
}
