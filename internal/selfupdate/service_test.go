package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

type rewriteTransport struct {
	destination string
	base        http.RoundTripper
}

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	parsed, _ := clone.URL.Parse(transport.destination + clone.URL.Path)
	clone.URL = parsed
	return transport.base.RoundTrip(clone)
}

func TestCheckSelectsVerifiedPrereleaseArtifact(t *testing.T) {
	t.Parallel()
	manifestBody := validManifest("0.4.0-beta.10")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "update-manifest.json") {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(manifestBody)
			return
		}
		writer.Header().Set("Content-Type", "application/atom+xml")
		_, _ = fmt.Fprint(writer, releaseFeed(
			atomTestEntry("v0.4.0-beta.2", "older", "", "2026-08-10T00:00:00Z"),
			atomTestEntry("v0.4.0-beta.10", "十号测试版", "&lt;p&gt;新增&lt;strong&gt;应用内更新&lt;/strong&gt;&lt;/p&gt;", "2026-08-14T00:00:00Z"),
		))
	}))
	defer server.Close()

	service := testService(server, "0.3.0-beta.2")
	info, err := service.Check(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || !info.CanInstall || info.LatestVersion != "0.4.0-beta.10" || info.Format != "nsis" || info.PlanID == "" || info.ReleaseNotes != "新增应用内更新" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestCheckUsesPublishedReleaseFeedWithoutGitHubAPIRateLimit(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if strings.HasSuffix(request.URL.Path, "update-manifest.json") {
			_, _ = writer.Write(validManifest("0.4.0-beta.1"))
			return
		}
		if strings.Contains(request.URL.Path, "/api/") {
			writer.Header().Set("X-RateLimit-Remaining", "0")
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = fmt.Fprint(writer, releaseFeed(atomTestEntry(
			"v0.4.0-beta.1", "Osverse v0.4.0-beta.1", "&lt;p&gt;rate-limit free&lt;/p&gt;", "2026-08-14T00:00:00Z",
		)))
	}))
	defer server.Close()
	service := testService(server, "0.3.0-beta.1")
	if info, err := service.Check(context.Background(), "", 0); err != nil || !info.Available || requests != 2 {
		t.Fatalf("Check() = (%#v, %v), requests=%d", info, err, requests)
	}
}

func TestReleaseFeedRejectsUntrustedEntryAndClassifiesRateLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/limited" {
			writer.Header().Set("X-RateLimit-Remaining", "0")
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = fmt.Fprint(writer, strings.Replace(
			releaseFeed(atomTestEntry("v0.4.0-beta.1", "release", "", "2026-08-14T00:00:00Z")),
			"https://github.com/Oswald-Hao/Osverse/releases/tag/", "https://evil.example/", 1,
		))
	}))
	defer server.Close()
	service := testService(server, "0.3.0-beta.1")
	if _, err := service.Check(context.Background(), "", 0); !errors.Is(err, ErrInvalidReply) {
		t.Fatalf("untrusted feed error = %v", err)
	}
	service.endpoint = server.URL + "/limited"
	if _, err := service.Check(context.Background(), "", 0); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate-limit error = %v", err)
	}
}

func TestStableChannelDoesNotSelectPrerelease(t *testing.T) {
	t.Parallel()
	current, _ := parseVersion("1.0.0")
	got, _, ok := selectRelease([]release{
		{TagName: "v1.1.0-beta.1", Prerelease: true},
		{TagName: "v1.0.1", Prerelease: false},
	}, current, false)
	if !ok || got.TagName != "v1.0.1" {
		t.Fatalf("selected %+v", got)
	}
}

func TestManifestRejectsUntrustedArtifactURL(t *testing.T) {
	t.Parallel()
	service := &Service{platform: "windows", architecture: "amd64"}
	document := manifest{SchemaVersion: 1, Repository: repository, Version: "1.2.3", Tag: "v1.2.3", Channel: "stable", Artifacts: []Artifact{{
		Architecture: "amd64", Filename: "osverse-1.2.3-windows-amd64-setup.exe", Format: "nsis", Platform: "windows", Target: "windows10+", Size: 10,
		SHA256: strings.Repeat("a", 64), URL: "https://evil.example/osverse.exe",
	}}}
	if _, _, _, err := service.selectAndValidateArtifact(document, "1.2.3"); err == nil {
		t.Fatal("untrusted URL accepted")
	}
}

func testService(server *httptest.Server, current string) *Service {
	service := NewService("/tmp", current)
	service.endpoint = server.URL + "/releases"
	service.platform = "windows"
	service.architecture = "amd64"
	service.client = func(proxyservice.Protocol, int) (*http.Client, error) {
		return &http.Client{Transport: rewriteTransport{destination: server.URL, base: http.DefaultTransport}}, nil
	}
	return service
}

func validManifest(version string) []byte {
	document := manifest{SchemaVersion: 1, Repository: repository, Version: version, Tag: "v" + version, Channel: "prerelease", Artifacts: []Artifact{{
		Architecture: "amd64", Filename: "osverse-" + version + "-windows-amd64-setup.exe", Format: "nsis", Platform: "windows", Target: "windows10+", Size: 1234,
		SHA256: strings.Repeat("a", 64), URL: "https://github.com/" + repository + "/releases/download/v" + version + "/osverse-" + version + "-windows-amd64-setup.exe",
	}}}
	result, _ := json.Marshal(document)
	return result
}

func releaseFeed(entries ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<feed xmlns="http://www.w3.org/2005/Atom">` +
		`<id>tag:github.com,2008:https://github.com/Oswald-Hao/Osverse/releases</id>` +
		strings.Join(entries, "") + `</feed>`
}

func atomTestEntry(tag, title, content, updated string) string {
	return `<entry><updated>` + updated + `</updated>` +
		`<link rel="alternate" type="text/html" href="https://github.com/Oswald-Hao/Osverse/releases/tag/` + tag + `"/>` +
		`<title>` + title + `</title><content type="html">` + content + `</content></entry>`
}
