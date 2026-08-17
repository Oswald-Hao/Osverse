package main

import (
	"os"
	"strings"
	"testing"
)

func TestDependabotCreatesFocusedPullRequestsAgainstDev(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	configuration := string(content)

	if strings.Contains(configuration, "\n    groups:") {
		t.Fatal("Dependabot must not bundle unrelated dependency upgrades into grouped pull requests")
	}
	if got := strings.Count(configuration, "  - package-ecosystem:"); got != 3 {
		t.Fatalf("Dependabot contains %d package ecosystems; want 3", got)
	}
	if got := strings.Count(configuration, "    target-branch: dev"); got != 3 {
		t.Fatalf("Dependabot contains %d dev targets; want one for every ecosystem", got)
	}
}
