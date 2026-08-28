//go:build !windows

package geminiinstall

import (
	"os"
	"path/filepath"
)

func managedPathsFor(home, goos, version string) managedPaths {
	root := filepath.Join(home, ".local", "share", "osverse")
	if goos == "darwin" {
		root = filepath.Join(home, "Library", "Application Support", "Osverse")
	}
	toolRoot := filepath.Join(root, "tools", componentID)
	finalRoot := filepath.Join(toolRoot, version)
	return managedPaths{
		root: root, stagingRoot: filepath.Join(root, "staging"), toolRoot: toolRoot,
		finalRoot: finalRoot, currentPath: filepath.Join(toolRoot, "current"),
		binRoot: filepath.Join(home, ".local", "bin"), shimPath: filepath.Join(home, ".local", "bin", commandName),
		wrapperPath: filepath.Join(finalRoot, "bin", commandName),
	}
}

func commitRename(source, destination string) error { return os.Rename(source, destination) }
