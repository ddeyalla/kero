package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestCodeChallenge(t *testing.T) {
	// The example of RFC 7636, appendix B.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := codeChallenge(verifier); got != challenge {
		t.Fatalf("challenge = %q, want %q", got, challenge)
	}
}

func TestNewVerifier(t *testing.T) {
	verifier, err := newVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) != 43 {
		t.Fatalf("verifier length = %d, want 43", len(verifier))
	}
	if _, err := base64.RawURLEncoding.DecodeString(verifier); err != nil {
		t.Fatalf("verifier is not base64url: %v", err)
	}
	other, err := newVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if verifier == other {
		t.Fatal("two verifiers are equal")
	}
	state, err := newState()
	if err != nil {
		t.Fatal(err)
	}
	if state == "" || state == verifier {
		t.Fatalf("state = %q", state)
	}
}

// anyPort is a Config.Listen that listens on a free loopback port, so
// that the tests do not need DefaultRedirectPort to be free.
func anyPort(network, address string) (net.Listener, error) {
	return net.Listen(network, "127.0.0.1:0")
}

// browser is an OpenURL that does what the browser does: it visits the
// loopback redirect in the test, so the callback gets its code. With
// again set, it visits the redirect a second time with that code.
type browser struct {
	t          *testing.T
	urls       []string
	code       string
	again      string
	state      string
	params     url.Values
	openErr    error
	lastStatus int
	statuses   []int
}

func (b *browser) OpenURL(raw string) error {
	b.urls = append(b.urls, raw)
	if b.openErr != nil {
		return b.openErr
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	q := u.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		return err
	}
	state := q.Get("state")
	if b.state != "" {
		state = b.state
	}
	codes := []string{b.code}
	if b.again != "" {
		codes = append(codes, b.again)
	}
	for _, code := range codes {
		callback := redirect.Query()
		callback.Set("state", state)
		callback.Set("code", code)
		for key, values := range b.params {
			for _, value := range values {
				callback.Set(key, value)
			}
		}
		redirect.RawQuery = callback.Encode()
		resp, err := http.Get(redirect.String())
		if err != nil {
			return err
		}
		b.lastStatus = resp.StatusCode
		b.statuses = append(b.statuses, resp.StatusCode)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return nil
}

func testNow() time.Time {
	return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
}

func TestSignIn(t *testing.T) {
	now := testNow()
	var form url.Values
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/token" {
			t.Errorf("token path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"access-1","token_type":"Bearer","scope":"playlist-read-private user-library-read user-read-playback-state","expires_in":3600,"refresh_token":"refresh-1"}`)
	}))
	defer accounts.Close()

	secrets := MapSecrets{}
	b := &browser{t: t, code: "the-code"}
	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		OpenURL:     b,
		Secrets:     secrets,
		Now:         func() time.Time { return now },
		Listen:      anyPort,
	})
	if err := client.SignIn(context.Background()); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if len(b.urls) != 1 {
		t.Fatalf("opened %d URLs, want 1", len(b.urls))
	}
	authorize, err := url.Parse(b.urls[0])
	if err != nil {
		t.Fatal(err)
	}
	if authorize.Path != "/authorize" {
		t.Errorf("authorize path = %q", authorize.Path)
	}
	q := authorize.Query()
	if got := q.Get("client_id"); got != "client-id" {
		t.Errorf("client_id = %q", got)
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q", got)
	}
	if got := q.Get("scope"); got != strings.Join(DefaultScopes, " ") {
		t.Errorf("scope = %q", got)
	}
	if got := q.Get("state"); got == "" {
		t.Error("state is empty")
	}
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Scheme != "http" || redirect.Path != callbackPath {
		t.Errorf("redirect_uri = %q", redirect)
	}
	if host, _, ok := strings.Cut(redirect.Host, ":"); !ok || host != "127.0.0.1" {
		t.Errorf("redirect host = %q", redirect.Host)
	}

	if got := form.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q", got)
	}
	if got := form.Get("code"); got != "the-code" {
		t.Errorf("code = %q", got)
	}
	if got := form.Get("client_id"); got != "client-id" {
		t.Errorf("token client_id = %q", got)
	}
	if got := form.Get("redirect_uri"); got != q.Get("redirect_uri") {
		t.Errorf("token redirect_uri = %q, authorize redirect_uri = %q", got, q.Get("redirect_uri"))
	}
	if got := form.Get("code_verifier"); got == "" {
		t.Error("code_verifier is empty")
	} else if want := q.Get("code_challenge"); codeChallenge(got) != want {
		t.Errorf("code_verifier does not match code_challenge %q", want)
	}

	var token Token
	if err := json.Unmarshal([]byte(secrets[TokensKey]), &token); err != nil {
		t.Fatalf("tokens secret: %v", err)
	}
	if token.AccessToken != "access-1" || token.RefreshToken != "refresh-1" {
		t.Errorf("saved token = %+v", token)
	}
	if !token.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("expires_at = %v", token.ExpiresAt)
	}
	if !client.SignedIn() {
		t.Error("SignedIn = false")
	}
	if got := client.Current(); got != StateReady {
		t.Errorf("Current = %q", got)
	}
	if b.lastStatus != http.StatusOK {
		t.Errorf("callback status = %d", b.lastStatus)
	}
}

func TestSignInWithoutClientID(t *testing.T) {
	b := &browser{t: t, code: "the-code"}
	client := New(Config{OpenURL: b, Secrets: MapSecrets{}})
	if err := client.SignIn(context.Background()); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("SignIn = %v, want ErrNoClientID", err)
	}
	if len(b.urls) != 0 {
		t.Fatalf("opened %d URLs, want 0", len(b.urls))
	}
}

func TestSignInOpenBrowserFails(t *testing.T) {
	b := &browser{t: t, openErr: errors.New("no browser")}
	client := New(Config{ClientID: "client-id", OpenURL: b, Secrets: MapSecrets{}, Listen: anyPort})
	if err := client.SignIn(context.Background()); !errors.Is(err, ErrSignInFailed) {
		t.Fatalf("SignIn = %v, want ErrSignInFailed", err)
	}
}

func TestSignInWrongState(t *testing.T) {
	b := &browser{t: t, code: "the-code", state: "not-the-state"}
	client := New(Config{
		ClientID:      "client-id",
		OpenURL:       b,
		Secrets:       MapSecrets{},
		SignInTimeout: 200 * time.Millisecond,
		Listen:        anyPort,
	})
	err := client.SignIn(context.Background())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SignIn = %v, want the timeout", err)
	}
	if b.lastStatus != http.StatusBadRequest {
		t.Fatalf("callback status = %d, want 400", b.lastStatus)
	}
}

func TestSignInDenied(t *testing.T) {
	var requests int
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer accounts.Close()

	b := &browser{t: t, code: "the-code", params: url.Values{"error": {"access_denied"}}}
	client := New(Config{ClientID: "client-id", AccountsURL: accounts.URL, OpenURL: b, Secrets: MapSecrets{}, Listen: anyPort})
	err := client.SignIn(context.Background())
	if !errors.Is(err, ErrSignInFailed) {
		t.Fatalf("SignIn = %v, want ErrSignInFailed", err)
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error = %v", err)
	}
	if requests != 0 {
		t.Errorf("token requests = %d, want 0", requests)
	}
}

func TestSignInTokenRequestFails(t *testing.T) {
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid_grant","error_description":"code expired"}`)
	}))
	defer accounts.Close()

	b := &browser{t: t, code: "the-code"}
	client := New(Config{ClientID: "client-id", AccountsURL: accounts.URL, OpenURL: b, Secrets: MapSecrets{}, Listen: anyPort})
	if err := client.SignIn(context.Background()); !errors.Is(err, ErrTokenRefused) {
		t.Fatalf("SignIn = %v, want ErrTokenRefused", err)
	}
}

func TestSignOutWaitsForSignIn(t *testing.T) {
	now := testNow()
	tokenStarted := make(chan struct{})
	releaseToken := make(chan struct{})
	var once sync.Once
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(tokenStarted) })
		<-releaseToken
		io.WriteString(w, `{"access_token":"access-1","expires_in":3600,"refresh_token":"refresh-1"}`)
	}))
	defer accounts.Close()

	secrets := MapSecrets{}
	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		OpenURL:     &browser{t: t, code: "the-code"},
		Secrets:     secrets,
		Now:         func() time.Time { return now },
		Listen:      anyPort,
	})

	signInErr := make(chan error, 1)
	go func() { signInErr <- client.SignIn(context.Background()) }()
	<-tokenStarted

	signOutErr := make(chan error, 1)
	go func() { signOutErr <- client.SignOut() }()
	select {
	case err := <-signOutErr:
		t.Fatalf("SignOut returned before the sign-in finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseToken)
	if err := <-signInErr; err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if err := <-signOutErr; err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if client.SignedIn() {
		t.Error("SignedIn = true after SignOut")
	}
	if got := secrets[TokensKey]; got != "" {
		t.Errorf("tokens secret = %q after SignOut", got)
	}
}

func TestRedirectURI(t *testing.T) {
	client := New(Config{ClientID: "client-id"})
	if got, want := client.RedirectURI(), "http://127.0.0.1:43017/callback"; got != want {
		t.Errorf("RedirectURI = %q, want %q", got, want)
	}
	other := New(Config{ClientID: "client-id", RedirectPort: 8888})
	if got, want := other.RedirectURI(), "http://127.0.0.1:8888/callback"; got != want {
		t.Errorf("RedirectURI = %q, want %q", got, want)
	}
}

// tokenServer is an accounts server that gives tokens for every code and
// records the codes it got.
func tokenServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var codes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		mu.Lock()
		codes = append(codes, r.PostForm.Get("code"))
		mu.Unlock()
		io.WriteString(w, `{"access_token":"access-1","expires_in":3600,"refresh_token":"refresh-1"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(codes)
	}
}

func TestSignInListensOnTheDefaultPort(t *testing.T) {
	accounts, _ := tokenServer(t)
	var addresses []string
	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		OpenURL:     &browser{t: t, code: "the-code"},
		Secrets:     MapSecrets{},
		Listen: func(network, address string) (net.Listener, error) {
			addresses = append(addresses, address)
			return anyPort(network, address)
		},
	})
	if err := client.SignIn(context.Background()); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if len(addresses) != 1 || addresses[0] != "127.0.0.1:43017" {
		t.Errorf("listened on %v, want 127.0.0.1:43017", addresses)
	}
}

func TestSignInPortBusy(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	port := held.Addr().(*net.TCPAddr).Port

	b := &browser{t: t, code: "the-code"}
	client := New(Config{ClientID: "client-id", RedirectPort: port, OpenURL: b, Secrets: MapSecrets{}})
	err = client.SignIn(context.Background())
	if !errors.Is(err, ErrRedirectPortBusy) {
		t.Fatalf("SignIn = %v, want ErrRedirectPortBusy", err)
	}
	if !errors.Is(err, ErrSignInFailed) {
		t.Errorf("SignIn = %v, want ErrSignInFailed too", err)
	}
	if len(b.urls) != 0 {
		t.Errorf("opened %d URLs, want 0", len(b.urls))
	}

	// A listener hook that reports the same failure gives the same error.
	hooked := New(Config{
		ClientID: "client-id",
		OpenURL:  b,
		Secrets:  MapSecrets{},
		Listen: func(network, address string) (net.Listener, error) {
			return nil, &net.OpError{Op: "listen", Net: network, Err: syscall.EADDRINUSE}
		},
	})
	if err := hooked.SignIn(context.Background()); !errors.Is(err, ErrRedirectPortBusy) {
		t.Errorf("SignIn = %v, want ErrRedirectPortBusy", err)
	}
}

func TestSecondCallbackIsIgnored(t *testing.T) {
	accounts, codes := tokenServer(t)
	b := &browser{t: t, code: "first", again: "second"}
	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		OpenURL:     b,
		Secrets:     MapSecrets{},
		Listen:      anyPort,
	})
	if err := client.SignIn(context.Background()); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got := codes(); len(got) != 1 || got[0] != "first" {
		t.Errorf("exchanged codes = %v, want only the first", got)
	}
	if len(b.statuses) != 2 || b.statuses[0] != http.StatusOK || b.statuses[1] != http.StatusConflict {
		t.Errorf("callback statuses = %v, want 200 then 409", b.statuses)
	}
}

// closeListener records whether the sign-in closed its listener.
type closeListener struct {
	net.Listener
	closed atomic.Bool
}

func (l *closeListener) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
}

func TestSignInClosesTheListener(t *testing.T) {
	accounts, _ := tokenServer(t)
	var ln *closeListener
	b := &browser{t: t, code: "the-code"}
	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		OpenURL:     b,
		Secrets:     MapSecrets{},
		Listen: func(network, address string) (net.Listener, error) {
			inner, err := anyPort(network, address)
			if err != nil {
				return nil, err
			}
			ln = &closeListener{Listener: inner}
			return ln, nil
		},
	})
	if err := client.SignIn(context.Background()); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if ln == nil || !ln.closed.Load() {
		t.Fatal("the listener is still open after SignIn")
	}
	if conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Error("the redirect port still takes connections after SignIn")
	}
}

func TestSignInClearsLastResults(t *testing.T) {
	accounts, _ := tokenServer(t)
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"items": [{"id": "p1", "name": "One"}]}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		OpenURL:     &browser{t: t, code: "the-code"},
		Secrets:     seededSecrets(t, liveToken(now)),
		Now:         func() time.Time { return now },
		Listen:      anyPort,
	})
	if _, err := client.Playlists(context.Background()); err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if err := client.SignIn(context.Background()); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if last, ok := client.LastPlaylists(); ok {
		t.Errorf("LastPlaylists = %+v after a sign-in, want none", last)
	}
}
