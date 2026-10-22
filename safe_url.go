package main

import "net/url"

// Remote issue content can only open web links, never local files or
// command/custom URL handlers.
func linearSafeURL(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
