package selfupdate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const (
	maxReleasesBytes = 2 << 20
	maxManifestBytes = 256 << 10
	maxReleaseNotes  = 32 << 10
	maxArtifactBytes = 512 << 20
	planLifetime     = 15 * time.Minute
)

var (
	ErrNoPlan       = errors.New("update plan is unavailable")
	ErrInvalidReply = errors.New("invalid update metadata")
)

type clientFactory func(proxyservice.Protocol, int) (*http.Client, error)

type Service struct {
	mu           sync.Mutex
	home         string
	currentRaw   string
	endpoint     string
	client       clientFactory
	now          func() time.Time
	platform     string
	architecture string
	plans        map[string]plan
}

func NewService(home, currentVersion string) *Service {
	return &Service{
		home: home, currentRaw: currentVersion, endpoint: releasesEndpoint,
		client: proxyservice.NewHTTPClient, now: time.Now, platform: runtime.GOOS,
		architecture: runtime.GOARCH, plans: make(map[string]plan),
	}
}

type releaseAsset struct {
	Name               string `json:"name"`
	State              string `json:"state"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	PublishedAt time.Time      `json:"published_at"`
	Assets      []releaseAsset `json:"assets"`
}

func (service *Service) Check(ctx context.Context, protocol proxyservice.Protocol, port int) (Info, error) {
	if service == nil {
		return Info{}, errors.New("update service unavailable")
	}
	current, err := parseVersion(service.currentRaw)
	if err != nil {
		return Info{CurrentVersion: service.currentRaw, Platform: service.platform, Message: "开发版本不参与自动更新"}, nil
	}
	client, err := service.secureClient(protocol, port)
	if err != nil {
		return Info{}, err
	}
	releases, err := service.fetchReleases(ctx, client)
	if err != nil {
		return Info{}, err
	}
	candidate, candidateVersion, ok := selectRelease(releases, current, len(current.pre) > 0)
	if !ok {
		return Info{CurrentVersion: service.currentRaw, LatestVersion: service.currentRaw, Platform: service.platform, Message: "当前已是最新版本"}, nil
	}
	manifestAsset, ok := findManifestAsset(candidate, candidateVersion)
	if !ok {
		return Info{}, ErrInvalidReply
	}
	document, err := service.fetchManifest(ctx, client, manifestAsset.BrowserDownloadURL, manifestAsset.Name, candidateVersion, manifestAsset.Size)
	if err != nil {
		return Info{}, err
	}
	artifact, installable, message, err := service.selectAndValidateArtifact(document, candidateVersion)
	if err != nil {
		return Info{}, err
	}
	info := Info{
		Available: true, CanInstall: installable, CurrentVersion: service.currentRaw,
		LatestVersion: candidateVersion, ReleaseName: strings.TrimSpace(candidate.Name),
		ReleaseNotes: trimUTF8(candidate.Body, maxReleaseNotes), PublishedAt: candidate.PublishedAt,
		DownloadBytes: artifact.Size, Platform: service.platform, Format: artifact.Format, Message: message,
	}
	if info.ReleaseName == "" {
		info.ReleaseName = "Osverse v" + candidateVersion
	}
	if installable {
		info.PlanID, err = randomID()
		if err != nil {
			return Info{}, err
		}
		service.mu.Lock()
		service.prunePlansLocked()
		service.plans[info.PlanID] = plan{artifact: artifact, info: info, expires: service.now().Add(planLifetime)}
		service.mu.Unlock()
	}
	return info, nil
}

func (service *Service) secureClient(protocol proxyservice.Protocol, port int) (*http.Client, error) {
	client, err := service.client(protocol, port)
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if request.URL.Scheme != "https" || !allowedDownloadHost(request.URL.Hostname()) {
			return errors.New("untrusted update redirect")
		}
		return nil
	}
	return client, nil
}

func allowedDownloadHost(host string) bool {
	switch strings.ToLower(host) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	default:
		return false
	}
}

func (service *Service) fetchReleases(ctx context.Context, client *http.Client) ([]release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "Osverse/"+service.currentRaw)
	var result []release
	if err := fetchJSON(client, request, maxReleasesBytes, false, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) fetchManifest(ctx context.Context, client *http.Client, rawURL, filename, version string, size int64) (manifest, error) {
	expected := "https://github.com/" + repository + "/releases/download/v" + version + "/" + url.PathEscape(filename)
	if size <= 0 || size > maxManifestBytes || !validReleaseURL(rawURL, filename) || rawURL != expected {
		return manifest{}, ErrInvalidReply
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return manifest{}, err
	}
	request.Header.Set("User-Agent", "Osverse/"+service.currentRaw)
	var result manifest
	if err := fetchJSON(client, request, maxManifestBytes, true, &result); err != nil {
		return manifest{}, err
	}
	return result, nil
}

func fetchJSON(client *http.Client, request *http.Request, limit int64, strict bool, destination any) error {
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("update server returned %d", response.StatusCode)
	}
	reader := &io.LimitedReader{R: response.Body, N: limit + 1}
	decoder := json.NewDecoder(reader)
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(destination); err != nil {
		return ErrInvalidReply
	}
	if reader.N <= 0 {
		return ErrInvalidReply
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ErrInvalidReply
	}
	return nil
}

func selectRelease(releases []release, current version, includePrerelease bool) (release, string, bool) {
	var selected release
	var selectedVersion version
	var selectedRaw string
	found := false
	for _, candidate := range releases {
		if candidate.Draft || (candidate.Prerelease && !includePrerelease) {
			continue
		}
		parsed, err := parseVersion(candidate.TagName)
		if err != nil || compareVersions(parsed, current) <= 0 {
			continue
		}
		if candidate.Prerelease != (len(parsed.pre) > 0) {
			continue
		}
		if !found || compareVersions(parsed, selectedVersion) > 0 {
			selected, selectedVersion, selectedRaw, found = candidate, parsed, strings.TrimPrefix(candidate.TagName, "v"), true
		}
	}
	return selected, selectedRaw, found
}

func findManifestAsset(candidate release, version string) (releaseAsset, bool) {
	expected := "osverse-" + version + "-update-manifest.json"
	for _, asset := range candidate.Assets {
		if asset.Name == expected && asset.State == "uploaded" {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func (service *Service) selectAndValidateArtifact(document manifest, version string) (Artifact, bool, string, error) {
	if document.SchemaVersion != 1 || document.Repository != repository || document.Version != version || document.Tag != "v"+version {
		return Artifact{}, false, "", ErrInvalidReply
	}
	parsed, _ := parseVersion(version)
	expectedChannel := "stable"
	if len(parsed.pre) > 0 {
		expectedChannel = "prerelease"
	}
	if document.Channel != expectedChannel {
		return Artifact{}, false, "", ErrInvalidReply
	}
	format, target, supported := desiredArtifact(service.platform, service.architecture)
	if !supported {
		return Artifact{}, false, "当前平台尚无可自动安装的发布包", nil
	}
	var selected *Artifact
	for index := range document.Artifacts {
		artifact := document.Artifacts[index]
		if artifact.Platform != service.platform || artifact.Architecture != "amd64" || artifact.Format != format {
			continue
		}
		if target != "" && artifact.Target != target {
			continue
		}
		if selected != nil {
			return Artifact{}, false, "", ErrInvalidReply
		}
		if err := validateArtifact(artifact, version, target); err != nil {
			return Artifact{}, false, "", err
		}
		selected = &artifact
	}
	if selected != nil {
		return *selected, true, updateMessage(format), nil
	}
	return Artifact{}, false, "当前系统没有匹配的更新包", nil
}

func validateArtifact(artifact Artifact, version, target string) error {
	if artifact.Size <= 0 || artifact.Size > maxArtifactBytes || len(artifact.SHA256) != 64 {
		return ErrInvalidReply
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil || artifact.SHA256 != strings.ToLower(artifact.SHA256) {
		return ErrInvalidReply
	}
	if !validReleaseURL(artifact.URL, artifact.Filename) {
		return ErrInvalidReply
	}
	expectedPrefix := "https://github.com/" + repository + "/releases/download/v" + version + "/"
	if artifact.URL != expectedPrefix+url.PathEscape(artifact.Filename) {
		return ErrInvalidReply
	}
	if strings.ContainsAny(artifact.Filename, `/\\`) || artifact.Filename == "" {
		return ErrInvalidReply
	}
	if artifact.Filename != expectedArtifactFilename(artifact.Format, version, target) {
		return ErrInvalidReply
	}
	return nil
}

func expectedArtifactFilename(format, version, target string) string {
	switch format {
	case "nsis":
		return "osverse-" + version + "-windows-amd64-setup.exe"
	case "appimage":
		return "osverse-" + version + "-linux-amd64-" + target + ".AppImage"
	case "tar.gz":
		return "osverse-" + version + "-linux-amd64-" + target + ".tar.gz"
	case "deb":
		return "osverse_" + version + "_amd64_" + target + ".deb"
	case "dmg":
		return "osverse-" + version + "-macos-amd64.dmg"
	default:
		return ""
	}
}

func validReleaseURL(rawURL, expectedFilename string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	prefix := "/" + repository + "/releases/download/"
	if !strings.HasPrefix(parsed.EscapedPath(), prefix) {
		return false
	}
	if expectedFilename != "" && !strings.HasSuffix(parsed.EscapedPath(), "/"+url.PathEscape(expectedFilename)) {
		return false
	}
	return true
}

func desiredArtifact(platform, architecture string) (format, target string, supported bool) {
	if architecture != "amd64" {
		return "", "", false
	}
	switch platform {
	case "windows":
		return "nsis", "windows10+", true
	case "linux":
		return linuxArtifactPreference()
	case "darwin":
		return "dmg", "macos12+", true
	default:
		return "", "", false
	}
}

func updateMessage(format string) string {
	switch format {
	case "deb":
		return "下载并校验后将由系统软件安装器确认更新"
	case "nsis":
		return "下载并校验后将启动 Osverse 安装程序"
	default:
		return "下载并校验后将替换当前 Osverse 并重新启动"
	}
}

func trimUTF8(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "…"
}

func randomID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func (service *Service) prunePlansLocked() {
	now := service.now()
	for id, candidate := range service.plans {
		if !candidate.expires.After(now) {
			delete(service.plans, id)
		}
	}
}
