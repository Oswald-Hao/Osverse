package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

const pinnedWailsVersion = "2.14.0"

func TestWailsVersionPinsStayAligned(t *testing.T) {
	t.Parallel()

	type pinFile struct {
		pattern string
		count   int
	}
	files := map[string]pinFile{
		"go.mod": {`github\.com/wailsapp/wails/v2 v([0-9]+\.[0-9]+\.[0-9]+)`, 1},
		filepath.Join(".github", "workflows", "ci.yml"):            {`wails@v([0-9]+\.[0-9]+\.[0-9]+)`, 3},
		filepath.Join(".github", "workflows", "release-linux.yml"): {`wails@v([0-9]+\.[0-9]+\.[0-9]+)`, 3},
		"README.md":       {`(?:Wails |wails@v)([0-9]+\.[0-9]+\.[0-9]+)`, 5},
		"README.en.md":    {`(?:Wails |wails@v)([0-9]+\.[0-9]+\.[0-9]+)`, 5},
		"CONTRIBUTING.md": {`Wails ([0-9]+\.[0-9]+\.[0-9]+)`, 1},
		filepath.Join("docs", "testing", "windows-v1-acceptance.md"): {`Wails ([0-9]+\.[0-9]+\.[0-9]+)`, 1},
	}

	for name, pin := range files {
		name, pin := name, pin
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			matches := regexp.MustCompile(pin.pattern).FindAllStringSubmatch(string(content), -1)
			for _, match := range matches {
				if match[1] != pinnedWailsVersion {
					t.Errorf("%s uses Wails %s; want %s", name, match[1], pinnedWailsVersion)
				}
			}
			if len(matches) != pin.count {
				t.Errorf("%s contains %d Wails version pins; want %d", name, len(matches), pin.count)
			}
		})
	}
}
