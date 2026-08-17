package kimiinstall

import "testing"

func TestArtifactCatalogPinsEveryOfficialStandaloneTarget(t *testing.T) {
	want := map[string]string{
		"linux/amd64":   "c5af089d5ad34c27f2f26d5f93588ba3f656bf771911e5d43c85be95d3e1cbd4",
		"linux/arm64":   "345b5ac3354c3d3890e34cf8e50ee1ce81e5f3b719a1db506797e53e520099e6",
		"windows/amd64": "eefcd15ef3f35480221b758f60e9568d8166b2776190c24131f162a2f89b6e1b",
		"windows/arm64": "89b684be9eeae8f07106e27f650ddd6880900a99c6c30b5bb06a79cde58f0286",
		"darwin/amd64":  "560dca967a3609b7d46a5b9d95c364a958e35558af231660194f5d77de444b87",
		"darwin/arm64":  "14a09fb898742be77eb2bf41fc7fe0d78fdbdc73a4aa8fd3c80b04ebf6bee193",
	}
	if len(artifacts) != len(want) {
		t.Fatalf("artifact count = %d, want %d", len(artifacts), len(want))
	}
	for target, digest := range want {
		item, err := artifactForTarget(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if item.SHA256 != digest || item.Version != kimiVersion || item.Size <= 0 {
			t.Fatalf("%s artifact = %#v", target, item)
		}
	}
}
