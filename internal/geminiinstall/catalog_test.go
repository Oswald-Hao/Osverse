package geminiinstall

import "testing"

func TestCatalogPinsOfficialGeminiAndNodeArtifacts(t *testing.T) {
	for _, target := range []struct{ goos, goarch string }{{"linux", "amd64"}, {"windows", "amd64"}} {
		runtimeItem, pack, err := artifactsForTarget(target.goos, target.goarch)
		if err != nil {
			t.Fatalf("artifactsForTarget(%s/%s): %v", target.goos, target.goarch, err)
		}
		if runtimeItem.GOOS != target.goos || runtimeItem.GOARCH != target.goarch || runtimeItem.Size <= 0 || len(runtimeItem.SHA256) != 64 {
			t.Errorf("runtime artifact = %#v", runtimeItem)
		}
		if pack.URL != "https://registry.npmjs.org/@google/gemini-cli/-/gemini-cli-0.57.0.tgz" || pack.Size != 20_718_256 || len(pack.SHA256) != 64 {
			t.Errorf("package artifact = %#v", pack)
		}
	}
	for _, target := range []struct{ goos, goarch string }{{"linux", "arm64"}, {"darwin", "amd64"}, {"windows", "386"}} {
		if _, _, err := artifactsForTarget(target.goos, target.goarch); err == nil {
			t.Errorf("unsupported target %s/%s was accepted", target.goos, target.goarch)
		}
	}
}
