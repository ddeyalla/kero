package web

import "errors"

// The states the plugin shows for the Web API. The widget draws an empty
// state for the first five, and keeps the last results for
// StateNetworkDown.
const (
	StateReady         = ""
	StateNoClientID    = "no_client_id"
	StateSignedOut     = "signed_out"
	StateTokenRefused  = "token_refused"
	StatePremiumLapsed = "premium_lapsed"
	StateNetworkDown   = "network_down"
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
)

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
	}
	return StateFailed
}
