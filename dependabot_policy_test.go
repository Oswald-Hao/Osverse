package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependabotCreatesFocusedPullRequestsAgainstDev(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	configuration := string(content)

	if got := strings.Count(configuration, "  - package-ecosystem:"); got != 3 {
		t.Fatalf("Dependabot contains %d package ecosystems; want 3", got)
	}
	if got := strings.Count(configuration, "    target-branch: dev"); got != 3 {
		t.Fatalf("Dependabot contains %d dev targets; want one for every ecosystem", got)
	}

	type dependencyGroup struct {
		Patterns []string `yaml:"patterns"`
	}
	type updateRule struct {
		PackageEcosystem string                     `yaml:"package-ecosystem"`
		Directory        string                     `yaml:"directory"`
		Groups           map[string]dependencyGroup `yaml:"groups"`
	}
	var config struct {
		Updates []updateRule `yaml:"updates"`
	}
	if err := yaml.Unmarshal(content, &config); err != nil {
		t.Fatalf("parse Dependabot config: %v", err)
	}

	wantPatterns := []string{"react", "react-dom", "@types/react", "@types/react-dom"}
	foundReactGroup := false
	for _, update := range config.Updates {
		if update.PackageEcosystem == "npm" && update.Directory == "/frontend" {
			if len(update.Groups) != 1 {
				t.Fatalf("frontend npm updates define %d groups; want only react-runtime", len(update.Groups))
			}
			group, ok := update.Groups["react-runtime"]
			if !ok {
				t.Fatal("frontend npm updates do not define the react-runtime group")
			}
			if !slices.Equal(group.Patterns, wantPatterns) {
				t.Fatalf("react-runtime patterns = %q; want %q", group.Patterns, wantPatterns)
			}
			foundReactGroup = true
			continue
		}
		if len(update.Groups) != 0 {
			t.Fatalf("%s updates bundle unrelated dependencies into grouped pull requests", update.PackageEcosystem)
		}
	}
	if !foundReactGroup {
		t.Fatal("frontend npm Dependabot update rule not found")
	}
}
