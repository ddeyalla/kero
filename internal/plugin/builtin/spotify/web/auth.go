package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// callbackPath is the path of the loopback redirect URI.
const callbackPath = "/callback"

// refreshLeeway is how long before expiry the client refreshes a token,
// so that a call does not start with a token that is about to lapse.
const refreshLeeway = time.Minute

// newVerifier returns a PKCE code verifier: 32 random bytes in base64url,
// which is the 43 characters the standard allows at the least.
func newVerifier() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("spotify: random verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// codeChallenge is the S256 challenge of a verifier.
func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// newState returns the random state that ties the callback to this
// sign-in.
func newState() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("spotify: random state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// authorizeURL builds the accounts URL the browser opens.
func (c *Client) authorizeURL(clientID, redirectURI, challenge, state string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", challenge)
	q.Set("state", state)
	q.Set("scope", strings.Join(c.scopes, " "))
	return c.accountsURL + "/authorize?" + q.Encode()
}

// callbackResult is what the loopback callback got: a code, or the error
// Spotify sent instead.
type callbackResult struct {
	code    string
	message string
}

// callback is the loopback server that waits for the redirect.
type callback struct {
	ln     net.Listener
	state  string
	result chan callbackResult
	once   sync.Once
	srv    *http.Server
}

func newCallback(ln net.Listener, state string) *callback {
	cb := &callback{ln: ln, state: state, result: make(chan callbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, cb.handle)
	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return cb
}

func (cb *callback) serve() { _ = cb.srv.Serve(cb.ln) }

func (cb *callback) close() { _ = cb.srv.Close() }

// wait gives the callback, or the context's reason for giving up.
func (cb *callback) wait(ctx context.Context) (callbackResult, error) {
	select {
	case res := <-cb.result:
		return res, nil
	case <-ctx.Done():
		return callbackResult{}, ctx.Err()
	}
}

func (cb *callback) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("state") != cb.state {
		http.Error(w, "The sign-in state does not match.", http.StatusBadRequest)
		return
	}
	if message := q.Get("error"); message != "" {
		cb.once.Do(func() { cb.result <- callbackResult{message: message} })
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, signInFailedPage)
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Error(w, "The callback carries no code.", http.StatusBadRequest)
		return
	}
	cb.once.Do(func() { cb.result <- callbackResult{code: code} })
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, signInDonePage)
}

// The pages the browser shows after the callback. The app is the only
// reader, and the plugin has no catalog of its own.
const signInDonePage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Spotify</title></head>
<body style="font: 15px -apple-system, system-ui, sans-serif; margin: 4rem auto; max-width: 28rem; color: #222">
<p>Spotify sent the sign-in back to Shibu Code.</p>
<p>You can close this window and go back to Shibu Code.</p>
</body>
</html>`

const signInFailedPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Spotify</title></head>
<body style="font: 15px -apple-system, system-ui, sans-serif; margin: 4rem auto; max-width: 28rem; color: #222">
<p>Shibu Code could not sign in to Spotify.</p>
<p>You can close this window and try again from Shibu Code.</p>
</body>
</html>`

// SignIn runs the Authorization Code flow with PKCE: it opens the
// browser, waits for the loopback callback, exchanges the code, and
// saves the tokens. It returns ErrNoClientID when the user has set no
// Client ID.
func (c *Client) SignIn(ctx context.Context) error {
	if !c.signInMu.TryLock() {
		return fmt.Errorf("%w: another sign-in is running", ErrSignInFailed)
	}
	defer c.signInMu.Unlock()

	clientID := c.ClientID()
	if clientID == "" {
		return ErrNoClientID
	}
	if c.openURL == nil {
		return fmt.Errorf("%w: no browser", ErrSignInFailed)
	}
	verifier, err := newVerifier()
	if err != nil {
		return err
	}
	state, err := newState()
	if err != nil {
		return err
	}
	ln, err := c.listen("tcp", fmt.Sprintf("127.0.0.1:%d", c.redirectPort))
	if err != nil {
		return fmt.Errorf("%w: listen on the loopback: %v", ErrSignInFailed, err)
	}
	cb := newCallback(ln, state)
	go cb.serve()
	defer cb.close()

	ctx, cancel := context.WithTimeout(ctx, c.signInTimeout)
	defer cancel()

	redirectURI := "http://" + ln.Addr().String() + callbackPath
	challenge := codeChallenge(verifier)
	if err := c.openURL.OpenURL(c.authorizeURL(clientID, redirectURI, challenge, state)); err != nil {
		return fmt.Errorf("%w: open the browser: %v", ErrSignInFailed, err)
	}
	res, err := cb.wait(ctx)
	if err != nil {
		return fmt.Errorf("spotify: wait for the sign-in: %w", err)
	}
	if res.message != "" {
		return fmt.Errorf("%w: %s", ErrSignInFailed, res.message)
	}
	return c.exchange(ctx, clientID, redirectURI, res.code, verifier)
}

// exchange trades the code for tokens and saves them.
func (c *Client) exchange(ctx context.Context, clientID, redirectURI, code, verifier string) error {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	var out tokenResponse
	if err := c.tokenRequest(ctx, form, &out); err != nil {
		return err
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return fmt.Errorf("%w: Spotify returned no token", ErrSignInFailed)
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	t := out.token(c.now())
	c.tokens = &t
	c.tokenLoaded = true
	return c.saveTokensLocked(&t)
}

// Token is what the client keeps between runs. It is a secret.
type Token struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// tokenResponse is one answer of the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// token turns an endpoint answer into the token to keep.
func (r tokenResponse) token(now time.Time) Token {
	seconds := r.ExpiresIn
	if seconds <= 0 {
		// Spotify always gives the lifetime; keep an hour if it does not.
		seconds = 3600
	}
	return Token{
		AccessToken:  r.AccessToken,
		TokenType:    r.TokenType,
		Scope:        r.Scope,
		RefreshToken: r.RefreshToken,
		ExpiresAt:    now.Add(time.Duration(seconds) * time.Second),
	}
}

// tokenRequest posts a form to the token endpoint.
func (c *Client) tokenRequest(ctx context.Context, form url.Values, out *tokenResponse) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return networkError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return networkError(err)
	}
	if len(raw) > maxResponse {
		return fmt.Errorf("spotify: token response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := spotifyMessage(raw)
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(message), "invalid_grant") {
			return fmt.Errorf("%w: %s", ErrTokenRefused, message)
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%w: token request failed (HTTP %d)", ErrNetworkDown, resp.StatusCode)
		}
		return fmt.Errorf("spotify: token request failed (HTTP %d): %s", resp.StatusCode, message)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("spotify: token response: %v", err)
	}
	return nil
}

// AccessToken returns a valid access token, and refreshes it when it is
// near its expiry. It returns ErrSignedOut when the client has no tokens.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	if c.ClientID() == "" {
		return "", ErrNoClientID
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.accessTokenLocked(ctx)
}

func (c *Client) accessTokenLocked(ctx context.Context) (string, error) {
	t, err := c.loadTokensLocked()
	if err != nil {
		return "", err
	}
	if t == nil || (t.AccessToken == "" && t.RefreshToken == "") {
		return "", ErrSignedOut
	}
	if t.AccessToken != "" && c.now().Before(t.ExpiresAt.Add(-refreshLeeway)) {
		return t.AccessToken, nil
	}
	t, err = c.refreshLocked(ctx, t)
	if err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// refreshRefused gets a fresh token after the API refused the access
// token, so that one stale token does not look like a revoked grant.
func (c *Client) refreshRefused(ctx context.Context) (string, error) {
	if c.ClientID() == "" {
		return "", ErrNoClientID
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	t, err := c.loadTokensLocked()
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", ErrSignedOut
	}
	t, err = c.refreshLocked(ctx, t)
	if err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// refreshLocked redeems the refresh token. The caller holds tokenMu.
func (c *Client) refreshLocked(ctx context.Context, old *Token) (*Token, error) {
	clientID := c.ClientID()
	if clientID == "" {
		return nil, ErrNoClientID
	}
	if old.RefreshToken == "" {
		return nil, fmt.Errorf("%w: no refresh token", ErrTokenRefused)
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", old.RefreshToken)
	form.Set("client_id", clientID)
	var out tokenResponse
	if err := c.tokenRequest(ctx, form, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("%w: Spotify returned no access token", ErrTokenRefused)
	}
	t := out.token(c.now())
	if t.RefreshToken == "" {
		t.RefreshToken = old.RefreshToken
	}
	if t.Scope == "" {
		t.Scope = old.Scope
	}
	c.tokens = &t
	c.tokenLoaded = true
	if err := c.saveTokensLocked(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// loadTokensLocked reads the tokens from the secrets once. The caller
// holds tokenMu.
func (c *Client) loadTokensLocked() (*Token, error) {
	if c.tokenLoaded {
		return c.tokens, nil
	}
	raw, err := c.secrets.Get(TokensKey)
	if err != nil {
		return nil, fmt.Errorf("spotify: read the tokens: %w", err)
	}
	c.tokenLoaded = true
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var t Token
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		// A broken secret is the same as no tokens: sign in again.
		return nil, nil
	}
	c.tokens = &t
	return c.tokens, nil
}

// saveTokensLocked writes the tokens to the secrets. The caller holds
// tokenMu.
func (c *Client) saveTokensLocked(t *Token) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := c.secrets.Set(TokensKey, string(raw)); err != nil {
		return fmt.Errorf("spotify: save the tokens: %w", err)
	}
	return nil
}

// networkError wraps a transport failure as ErrNetworkDown. A canceled
// context is not a network failure: it is the caller going away.
func networkError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrNetworkDown, err)
}
