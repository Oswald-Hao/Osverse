package install

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltInManifestHasValidatedFixedArtifacts(t *testing.T) {
	value, err := builtInManifest()
	if err != nil {
		t.Fatalf("builtInManifest() error = %v", err)
	}
	if value.SchemaVersion != 1 || value.Sequence != 1 || len(value.Components) != 3 {
		t.Fatalf("manifest metadata = %#v", value)
	}
	want := map[string]string{
		"claude-code":  "cb0eafa919df3cfc4ff1de2ae3e27ff21a2f1149a5aad08f7850c40a4fd40f8e",
		"codex-cli":    "c969740cf8297e4c31905cd551efeb2c99af5080c12c236bdf825598b250139a",
		"opencode-cli": "44c88fc35dd1ba0c8863e729cb8b99ddb0de934f6b52c234e1872c01d05f9e60",
	}
	for _, item := range value.Components {
		if item.SHA256 != want[item.ID] {
			t.Errorf("%s hash = %q", item.ID, item.SHA256)
		}
		if !strings.HasPrefix(item.URL, "https://registry.npmjs.org/") {
			t.Errorf("%s URL = %q", item.ID, item.URL)
		}
	}
}

func TestSignedManifestVerifiesExactBytesBeforeStrictParsing(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := manifestFiles.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(privateKey, raw)
	if _, err := verifySignedManifest(raw, signature, publicKey); err != nil {
		t.Fatalf("verified manifest error = %v", err)
	}

	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] ^= 1
	if _, err := verifySignedManifest(tampered, signature, publicKey); err == nil {
		t.Fatal("tampered manifest verified")
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["unexpected"] = true
	unknown, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifySignedManifest(unknown, ed25519.Sign(privateKey, unknown), publicKey); err == nil {
		t.Fatal("signed manifest with unknown field was accepted")
	}
}

func TestManifestRejectsNonAllowlistedOrUnsafeArtifacts(t *testing.T) {
	base, err := builtInManifest()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*artifact)
	}{
		{name: "HTTP URL", mutate: func(item *artifact) { item.URL = "http://registry.npmjs.org/package.tgz" }},
		{name: "lookalike host", mutate: func(item *artifact) { item.URL = "https://registry.npmjs.org.evil.example/package.tgz" }},
		{name: "userinfo", mutate: func(item *artifact) { item.URL = "https://registry.npmjs.org@evil.example/package.tgz" }},
		{name: "traversal", mutate: func(item *artifact) { item.BinaryPath = "package/../../bin/tool" }},
		{name: "oversize", mutate: func(item *artifact) { item.DownloadBytes = maxArtifactBytes + 1 }},
		{name: "invalid hash", mutate: func(item *artifact) { item.SHA256 = strings.Repeat("0", 63) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Components = append([]artifact(nil), base.Components...)
			test.mutate(&value.Components[0])
			if err := validateManifest(value); err == nil {
				t.Fatal("unsafe artifact accepted")
			}
		})
	}
}
