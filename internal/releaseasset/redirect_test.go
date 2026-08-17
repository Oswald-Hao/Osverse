package releaseasset

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestGitHubRedirectPolicyAllowsOnlyOneOfficialHTTPSHandoff(t *testing.T) {
	original := "https://github.com/owner/repo/releases/download/v1/archive.tar.gz"
	policy := GitHubRedirectPolicy(original)
	first := requestFor(t, original)
	trusted := requestFor(t, "https://release-assets.githubusercontent.com/github-production-release-asset/1/file?sig=fixed")
	if err := policy(trusted, []*http.Request{first}); err != nil {
		t.Fatalf("trusted redirect rejected: %v", err)
	}

	for name, candidate := range map[string]string{
		"untrusted host": "https://example.invalid/file",
		"host suffix":    "https://release-assets.githubusercontent.com.evil.invalid/file",
		"plain HTTP":     "http://release-assets.githubusercontent.com/file",
		"explicit port":  "https://release-assets.githubusercontent.com:443/file",
		"userinfo":       "https://user@release-assets.githubusercontent.com/file",
		"fragment":       "https://release-assets.githubusercontent.com/file#fragment",
	} {
		t.Run(name, func(t *testing.T) {
			if err := policy(requestFor(t, candidate), []*http.Request{first}); !errors.Is(err, ErrUntrustedRedirect) {
				t.Fatalf("policy error = %v", err)
			}
		})
	}
	if err := policy(trusted, []*http.Request{first, trusted}); !errors.Is(err, ErrUntrustedRedirect) {
		t.Fatalf("second redirect error = %v", err)
	}
	tamperedFirst := requestFor(t, original+"?changed=1")
	if err := policy(trusted, []*http.Request{tamperedFirst}); !errors.Is(err, ErrUntrustedRedirect) {
		t.Fatalf("tampered source error = %v", err)
	}
}

func TestGitHubRedirectPolicyRejectsInvalidOriginal(t *testing.T) {
	policy := GitHubRedirectPolicy("https://example.invalid/file")
	if err := policy(requestFor(t, "https://release-assets.githubusercontent.com/file"), []*http.Request{requestFor(t, "https://example.invalid/file")}); !errors.Is(err, ErrUntrustedRedirect) {
		t.Fatalf("policy error = %v", err)
	}
}

func requestFor(t *testing.T, raw string) *http.Request {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Request{Method: http.MethodGet, URL: parsed}
}
