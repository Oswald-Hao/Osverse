package qweninstall

import "testing"

func TestArtifactCatalogPinsEveryOfficialStandaloneTarget(t *testing.T) {
	want := map[string]string{
		"linux/amd64":   "a58e9a99c2f9e706d262c2bcb918e1c62cc29b3af8b96a072d45d2e57edf3ba3",
		"linux/arm64":   "1aaff5737a86e984cae6079174ca395aa57e5d9c6d29dd94f0114334ebf21504",
		"windows/amd64": "1e2e7db98e5ae52fda85ae8fffb9c58504620ad4b5367cd5e479eff0e23debb1",
		"darwin/amd64":  "2e9568d2190e92fe10d2baadda21ffb47c35493fef010a85a2d8ae8b1c7d2cf8",
		"darwin/arm64":  "bf7e0e4c6c4b815a02398b63a12aaacde749f7b11076c75ffd9f3d8592693961",
	}
	for target, digest := range want {
		artifact, err := artifactForTarget(target)
		if err != nil {
			t.Fatalf("artifactForTarget(%q): %v", target, err)
		}
		if artifact.Version != "0.21.13" || artifact.SHA256 != digest || artifact.Size <= 0 {
			t.Fatalf("artifactForTarget(%q) = %#v", target, artifact)
		}
	}
	if _, err := artifactForTarget("windows/arm64"); err == nil {
		t.Fatal("unsupported target was accepted")
	}
}
