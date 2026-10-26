package engine

import (
	"fmt"
	"strings"
	"testing"
)

// jsField returns the value of a field of the page's script, from after
// "field:" to the comma or the closing brace that ends it, with the value's
// own brackets and strings skipped so that their commas do not end it early.
func jsField(page, field string) (string, bool) {
	at := strings.Index(page, field+":")
	if at < 0 {
		return "", false
	}
	start := at + len(field) + 1
	depth := 0
	var quote byte
	for i := start; i < len(page); i++ {
		c := page[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return strings.TrimSpace(page[start:i]), true
			}
			depth--
		case c == ',' && depth == 0:
			return strings.TrimSpace(page[start:i]), true
		}
	}
	return strings.TrimSpace(page[start:]), true
}

// jsArguments returns the text between the parenthesis at open and the one
// that closes it, with quoted text and nested calls skipped.
func jsArguments(src string, open int) (string, bool) {
	if open < 0 || open >= len(src) || src[open] != '(' {
		return "", false
	}
	depth := 0
	var quote byte
	for i := open; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return src[open+1 : i], true
			}
		}
	}
	return "", false
}

// oauthTokenAnswers tells whether the page's getOAuthToken handler answers
// the SDK's callback on both of its paths. The SDK waits for that callback
// before every connect, so a handler that only reports a failed token fetch
// leaves its player waiting for a token that never comes. The page is a
// script that a Go test cannot run, so this reads what it is given instead.
func oauthTokenAnswers(page string) error {
	handler, ok := jsField(page, "getOAuthToken")
	if !ok {
		return fmt.Errorf("the page has no getOAuthToken field")
	}
	thenAt := strings.Index(handler, ".then(")
	if thenAt < 0 {
		return fmt.Errorf("getOAuthToken does not read its token with .then: %s", handler)
	}
	success, ok := jsArguments(handler, thenAt+len(".then"))
	if !ok || !strings.Contains(success, "cb(") {
		return fmt.Errorf("the token success path does not answer the SDK's callback: %q", success)
	}
	catchAt := strings.Index(handler, ".catch(")
	if catchAt < 0 {
		return fmt.Errorf("getOAuthToken has no .catch: %s", handler)
	}
	failure, ok := jsArguments(handler, catchAt+len(".catch"))
	if !ok {
		return fmt.Errorf("getOAuthToken's .catch does not close: %s", handler)
	}
	if !strings.Contains(failure, "cb(") {
		return fmt.Errorf("a failed token fetch leaves the SDK's callback unanswered: %q", failure)
	}
	if !strings.Contains(failure, "fail(") {
		return fmt.Errorf("a failed token fetch is not reported to the plugin: %q", failure)
	}
	return nil
}

// TestTheOAuthTokenCallbackAlwaysAnswersTheSDK guards the page's
// getOAuthToken handler. A token fetch that fails must answer the SDK's
// callback, or the player waits for it and never reports ready or an error.
func TestTheOAuthTokenCallbackAlwaysAnswersTheSDK(t *testing.T) {
	if err := oauthTokenAnswers(string(pageHTML)); err != nil {
		t.Fatalf("the page's getOAuthToken: %v", err)
	}
	// The handler this fixes: it reported the failure and left the SDK
	// waiting. It must fail the check, so that a change back to it fails
	// here rather than in a pane that never starts.
	const before = `player = new Spotify.Player({
    getOAuthToken: cb => token().then(t => cb(t)).catch(e => fail('token', e)),
    volume: 0.5,
  });`
	if err := oauthTokenAnswers(before); err == nil {
		t.Fatal("a failed token fetch that leaves the SDK's callback unanswered passed")
	}
}
