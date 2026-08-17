package main

import (
	"os"
	"strings"
	"testing"
)

func TestCIEnforcesGoCoverageFloor(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(content)
	for _, required := range []string{
		"name: Run Go coverage regression",
		`go test -coverprofile="$coverage_profile" ./...`,
		"minimum_coverage=60.0",
		`coverage_total < minimum_coverage`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("CI workflow is missing coverage gate fragment %q", required)
		}
	}
}
