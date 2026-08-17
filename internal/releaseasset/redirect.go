// Package releaseasset contains the narrow network policy shared by verified
// installers that download immutable GitHub Release assets.
package releaseasset

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

var ErrUntrustedRedirect = errors.New("untrusted GitHub release asset redirect")

// GitHubRedirectPolicy permits GitHub's single HTTPS handoff to its dedicated
// release-asset host. Artifact size and digest verification remain mandatory
// at the caller; every other redirect is rejected.
func GitHubRedirectPolicy(original string) func(*http.Request, []*http.Request) error {
	expected, parseErr := url.Parse(original)
	return func(next *http.Request, via []*http.Request) error {
		if parseErr != nil || expected.Scheme != "https" || !strings.EqualFold(expected.Host, "github.com") {
			return ErrUntrustedRedirect
		}
		if next == nil || next.URL == nil || len(via) != 1 || via[0] == nil || via[0].URL == nil {
			return ErrUntrustedRedirect
		}
		previous := via[0].URL
		if previous.Scheme != expected.Scheme || !strings.EqualFold(previous.Host, expected.Host) || previous.EscapedPath() != expected.EscapedPath() || previous.RawQuery != expected.RawQuery {
			return ErrUntrustedRedirect
		}
		if next.Method != http.MethodGet || next.URL.Scheme != "https" || !strings.EqualFold(next.URL.Hostname(), "release-assets.githubusercontent.com") || next.URL.Port() != "" || next.URL.User != nil || next.URL.Fragment != "" {
			return ErrUntrustedRedirect
		}
		return nil
	}
}
