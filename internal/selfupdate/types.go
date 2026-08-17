package selfupdate

import "time"

const (
	repository       = "Oswald-Hao/Osverse"
	releasesEndpoint = "https://github.com/" + repository + "/releases.atom"
)

// Info is the frontend-safe result of an update check. PlanID is opaque and
// binds installation to the release metadata that the backend verified.
type Info struct {
	Available      bool      `json:"available"`
	CanInstall     bool      `json:"canInstall"`
	PlanID         string    `json:"planId"`
	CurrentVersion string    `json:"currentVersion"`
	LatestVersion  string    `json:"latestVersion"`
	ReleaseName    string    `json:"releaseName"`
	ReleaseNotes   string    `json:"releaseNotes"`
	PublishedAt    time.Time `json:"publishedAt"`
	DownloadBytes  int64     `json:"downloadBytes"`
	Platform       string    `json:"platform"`
	Format         string    `json:"format"`
	Message        string    `json:"message"`
}

// ApplyResult tells the UI what action was started. ShouldQuit is consumed by
// the Wails bridge and is not a frontend instruction.
type ApplyResult struct {
	Started    bool   `json:"started"`
	ShouldQuit bool   `json:"-"`
	Message    string `json:"message"`
}

type Artifact struct {
	Architecture string `json:"architecture"`
	Filename     string `json:"filename"`
	Format       string `json:"format"`
	Platform     string `json:"platform"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Target       string `json:"target"`
	URL          string `json:"url"`
}

type manifest struct {
	Artifacts     []Artifact `json:"artifacts"`
	Channel       string     `json:"channel"`
	Repository    string     `json:"repository"`
	SchemaVersion int        `json:"schemaVersion"`
	Tag           string     `json:"tag"`
	Version       string     `json:"version"`
}

type plan struct {
	artifact Artifact
	info     Info
	expires  time.Time
}
