package selfupdate

import (
	"context"
	"encoding/json"
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
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "update-manifest.json") {
			_, _ = writer.Write(manifestBody)
			return
		}
		releases := []map[string]any{
			{"tag_name": "v0.4.0-beta.2", "name": "older", "body": "", "draft": false, "prerelease": true, "published_at": "2026-08-10T00:00:00Z", "assets": []any{}},
			{"tag_name": "v0.4.0-beta.10", "name": "十号测试版", "body": "新增应用内更新", "draft": false, "prerelease": true, "published_at": "2026-08-14T00:00:00Z", "extra_github_field": true, "assets": []map[string]any{{"name": "osverse-0.4.0-beta.10-update-manifest.json", "state": "uploaded", "size": len(manifestBody), "browser_download_url": "https://github.com/Oswald-Hao/Osverse/releases/download/v0.4.0-beta.10/osverse-0.4.0-beta.10-update-manifest.json"}}},
		}
		_ = json.NewEncoder(writer).Encode(releases)
	}))
	defer server.Close()

	service := testService(server, "0.3.0-beta.2")
	info, err := service.Check(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || !info.CanInstall || info.LatestVersion != "0.4.0-beta.10" || info.Format != "nsis" || info.PlanID == "" {
		t.Fatalf("unexpected info: %+v", info)
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
