package web

import (
	"errors"
	"fmt"
	"time"
)

// The states the plugin shows for the Web API. The widget draws an empty
// state for the first five, and keeps the last results for
// StateNetworkDown and StateRateLimited.
const (
	StateReady         = ""
	StateNoClientID    = "no_client_id"
	StateSignedOut     = "signed_out"
	StateTokenRefused  = "token_refused"
	StatePremiumLapsed = "premium_lapsed"
	StateNetworkDown   = "network_down"
	StateRateLimited   = "rate_limited"
	StateFailed        = "failed"
)

// The failures with a state of their own. Every other failure is
// StateFailed, which the widget shows as one line.
var (
	ErrNoClientID    = errors.New("spotify: no Client ID")
	ErrSignedOut     = errors.New("spotify: not signed in")
	ErrTokenRefused  = errors.New("spotify: Spotify refused the token; sign in again")
	ErrPremiumLapsed = errors.New("spotify: Premium is required")
	ErrNetworkDown   = errors.New("spotify: the network is down")
	ErrSignInFailed  = errors.New("spotify: sign-in failed")
	// ErrRateLimited is Spotify asking the client to wait. The error a
	// call returns is a *RateLimitError, which tells until when.
	ErrRateLimited = errors.New("spotify: Spotify asked to wait (rate limit)")
	// ErrRedirectPortBusy is a sign-in that cannot listen on the redirect
	// port, because another program holds it. It comes together with
	// ErrSignInFailed.
	ErrRedirectPortBusy = errors.New("spotify: the redirect port is busy")
)

// RateLimitError is a call that Spotify refused with HTTP 429, or that
// the client did not send because the wait Spotify asked for has not
// passed yet. It matches ErrRateLimited.
type RateLimitError struct {
	// Until is when the client calls Spotify again.
	Until time.Time
	// Message is what Spotify said, "" when the client did not ask.
	Message string
}

func (e *RateLimitError) Error() string {
	s := fmt.Sprintf("%v until %s", ErrRateLimited, e.Until.Format(time.TimeOnly))
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// State is what a failure means for the plugin: the state it shows. A nil
// error is StateReady.
func State(err error) string {
	switch {
	case err == nil:
		return StateReady
	case errors.Is(err, ErrNoClientID):
		return StateNoClientID
	case errors.Is(err, ErrSignedOut):
		return StateSignedOut
	case errors.Is(err, ErrTokenRefused):
		return StateTokenRefused
	case errors.Is(err, ErrPremiumLapsed):
		return StatePremiumLapsed
	case errors.Is(err, ErrNetworkDown):
		return StateNetworkDown
	case errors.Is(err, ErrRateLimited):
		return StateRateLimited
	}
	return StateFailed
}

// keepsResults tells whether a call that failed with err returns the
// last results, so that the pane keeps them and adds a note.
func keepsResults(err error) bool {
	return errors.Is(err, ErrNetworkDown) || errors.Is(err, ErrRateLimited)
}
