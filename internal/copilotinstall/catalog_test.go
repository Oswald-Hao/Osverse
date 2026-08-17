package copilotinstall

import "testing"

func TestArtifactCatalogPinsEveryOfficialStandaloneTarget(t *testing.T) {
	want := map[string]string{
		"linux/amd64":   "039933c9247686131c4406abb1d439bdbf68103edc1ff585bd70d5b0dc940f72",
		"linux/arm64":   "3ed85e711955e13be523bf492bc6c93b40b69925bcb7f817c9d08abf4839cf89",
		"windows/amd64": "e9ea2063913faa8a9f1cf374529c5fea075da0545a894d7469026166f854c541",
		"darwin/amd64":  "a1a9c1f25740f9a27b34eb14b70b5d3175794dc8bb410875531aa198b3abc18f",
		"darwin/arm64":  "2346bb691981c2997d65c1c5bc3cef1aeddc9edd37dcb2f970b911aa597e59f6",
	}
	if len(artifacts) != len(want) {
		t.Fatalf("artifact count = %d, want %d", len(artifacts), len(want))
	}
	for target, digest := range want {
		item, err := artifactForTarget(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if item.SHA256 != digest || item.Version != copilotVersion || item.Size <= 0 {
			t.Fatalf("%s artifact = %#v", target, item)
		}
	}
}
