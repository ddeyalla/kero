// Package web is the Spotify Web API side of the Spotify plugin: the
// sign-in with PKCE, the tokens, and the calls for search, playlists and
// saved albums.
//
// The package has no UI. It gives the widget the data and a state for
// every way a call can fail: no Client ID, not signed in, a refused
// token, a lapsed Premium, a network that is down, and a rate limit.
// State reads that state from an error. When the network is down or
// Spotify asks to wait, a call returns the last results it got, so that
// the pane keeps them and adds a note.
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

// DefaultRedirectPort is the loopback port of the redirect URI when
// Config.RedirectPort is zero. The URI must match the one the user
// registered in the Spotify dashboard, so the port stays the same from
// one sign-in to the next.
const DefaultRedirectPort = 43017

// TokensKey is the plugin secret that keeps the tokens.
const TokensKey = "tokens"

// SettingClientID is the key of the plugin setting that holds the Client
// ID of the user's Spotify developer app.
const SettingClientID = "clientId"

// ScopeStreaming is the scope that lets the page of the playback engine
// play audio.
const ScopeStreaming = "streaming"

// ScopeRecentlyPlayed is the scope that lets the plugin read the last
// track the user played. Tokens from before it lack it, and the plugin
// does without; it never asks the user to sign in again for it.
const ScopeRecentlyPlayed = "user-read-recently-played"

// ScopeCollaborative is the scope that lets the plugin read the tracks of
// the collaborative playlists of the user. Tokens from before it lack it:
// PlaylistTracks then fails with a 403 for such a playlist, and the pane
// can ask the user to sign in again.
const ScopeCollaborative = "playlist-read-collaborative"

// DefaultScopes are the scopes the plugin asks for when Config.Scopes is
// empty: private playlists, saved albums and the playback state, and what
// the playback engine needs to play audio in the app: streaming, the
// account's email and plan, and control of playback, and the last track
// played.
var DefaultScopes = []string{
	"playlist-read-private",
	ScopeCollaborative,
	"user-library-read",
	"user-read-playback-state",
	ScopeStreaming,
	"user-read-email",
	"user-read-private",
	"user-modify-playback-state",
	ScopeRecentlyPlayed,
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
	// RedirectPort is the port of the loopback callback. Zero is
	// DefaultRedirectPort. RedirectURI gives the URI to register.
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
	// appID is the last Client ID that was not empty, so that clearing
	// the field and typing the same ID again keeps the tokens.
	appID string

	tokenMu     sync.Mutex
	tok         *Token
	tokenLoaded bool
	// refused is a refresh that Spotify refused. It holds until the next
	// sign-in, so that no call sends the refresh again.
	refused bool

	// loadMu makes the one-time read of the tokens secret single.
	loadMu sync.Mutex
	// refreshMu makes one refresh serve every caller.
	refreshMu sync.Mutex
	signInMu  sync.Mutex

	cacheMu sync.Mutex
	// gen counts the sign-ins, the sign-outs and the changes of app. A
	// call keeps its results only while gen is the one it started with.
	gen uint64
	// searchSeq numbers the searches, and lastSearchSeq is the search
	// that the cache holds, so that an older search cannot replace a
	// newer one.
	searchSeq        uint64
	lastSearchSeq    uint64
	lastSearch       SearchResult
	lastSearchSet    bool
	lastPlaylists    []Playlist
	lastPlaylistsSet bool
	lastAlbums       []Album
	lastAlbumsSet    bool
	// searches are the answers of the last searches, the newest last, so
	// that typing a query again shows its results at once. See
	// CachedSearch.
	searches []cachedSearch
	// trackLists are the track lists of the last playlists and albums
	// that were opened, the newest last. See trackListCacheSize.
	trackLists []trackList

	// flightMu guards flights: the library loads in flight, by name, so
	// that the panes of one plugin share one request.
	flightMu sync.Mutex
	flights  map[string]*flight

	// limitMu guards limitedUntil: the time before which the client
	// does not call Spotify, after a rate limit.
	limitMu      sync.Mutex
	limitedUntil time.Time
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
	port := cfg.RedirectPort
	if port == 0 {
		port = DefaultRedirectPort
	}
	clientID := strings.TrimSpace(cfg.ClientID)
	return &Client{
		baseURL:       base,
		accountsURL:   accounts,
		scopes:        scopes,
		redirectPort:  port,
		openURL:       cfg.OpenURL,
		secrets:       secrets,
		http:          httpClient,
		now:           now,
		signInTimeout: timeout,
		listen:        listen,
		clientID:      clientID,
		appID:         clientID,
	}
}

// RedirectURI is the redirect URI the sign-in uses, which the user
// registers in the Spotify dashboard.
func (c *Client) RedirectURI() string {
	return redirectURI(c.redirectPort)
}

// DefaultRedirectURI is the redirect URI of a client on
// DefaultRedirectPort, which the plugin uses, for a pane that tells the
// user what to register before it has a client.
func DefaultRedirectURI() string {
	return redirectURI(DefaultRedirectPort)
}

func redirectURI(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)
}

// ClientID returns the Client ID of the user's developer app, "" when
// the user set none.
func (c *Client) ClientID() string {
	c.clientMu.RLock()
	defer c.clientMu.RUnlock()
	return c.clientID
}

// SetClientID changes the Client ID, as the settings do.
//
// A Client ID of another app forgets the tokens, the last results and
// the rate limit, which belong to the old app. An empty Client ID keeps
// them, so that the same ID typed again is still signed in.
func (c *Client) SetClientID(id string) {
	id = strings.TrimSpace(id)
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	c.clientID = id
	if id == "" || id == c.appID {
		return
	}
	other := c.appID != ""
	c.appID = id
	if !other {
		return
	}

	// The change and the forgetting are one critical section under
	// clientMu, so a sign-in for the old app cannot adopt its tokens
	// in between. A refresh in flight finds other tokens and drops its
	// own.
	c.tokenMu.Lock()
	c.tok = nil
	c.tokenLoaded = true
	c.refused = false
	// The secret write is best effort: SetClientID has no error, and a
	// token that stays in the secret is refused at its next refresh.
	_ = c.secrets.Set(TokensKey, "")
	c.tokenMu.Unlock()

	c.resetCache()
	c.limitMu.Lock()
	c.limitedUntil = time.Time{}
	c.limitMu.Unlock()
}

// SignedIn tells whether the client holds tokens.
func (c *Client) SignedIn() bool {
	t, err := c.tokens()
	if err != nil {
		return false
	}
	return t != nil && (t.AccessToken != "" || t.RefreshToken != "")
}

// CanStream tells whether the client's tokens carry the streaming scope.
// Tokens from before the engine do not: the user signs in once more.
func (c *Client) CanStream() bool {
	t, err := c.tokens()
	if err != nil || t == nil {
		return false
	}
	return slices.Contains(strings.Fields(t.Scope), ScopeStreaming)
}

// HasScope tells whether the client's tokens carry a scope.
func (c *Client) HasScope(scope string) bool {
	t, err := c.tokens()
	if err != nil || t == nil {
		return false
	}
	return slices.Contains(strings.Fields(t.Scope), scope)
}

// Current is the state the plugin shows before a call: no Client ID, a
// refused token, not signed in, or ready.
func (c *Client) Current() string {
	if c.ClientID() == "" {
		return StateNoClientID
	}
	if c.isRefused() {
		return StateTokenRefused
	}
	if !c.SignedIn() {
		return StateSignedOut
	}
	return StateReady
}

// SignOut forgets the tokens and the last results.
//
// It waits for a sign-in to finish and for a refresh to answer, so that
// neither can bring the tokens back after they are gone.
func (c *Client) SignOut() error {
	c.signInMu.Lock()
	defer c.signInMu.Unlock()
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.loadMu.Lock()
	defer c.loadMu.Unlock()

	c.tokenMu.Lock()
	c.tok = nil
	c.tokenLoaded = true
	c.refused = false
	c.tokenMu.Unlock()

	c.resetCache()

	if err := c.secrets.Set(TokensKey, ""); err != nil {
		return fmt.Errorf("spotify: forget the tokens: %w", err)
	}
	return nil
}

// resetCache forgets the last results and starts a new generation, so
// that a call in flight cannot keep its results afterwards.
func (c *Client) resetCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.gen++
	c.lastSearch = SearchResult{}
	c.lastSearchSet = false
	c.lastSearchSeq = c.searchSeq
	c.lastPlaylists = nil
	c.lastPlaylistsSet = false
	c.lastAlbums = nil
	c.lastAlbumsSet = false
	c.searches = nil
	c.trackLists = nil
}
