//go:build !windows

package harnessinstall

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

func commitHarnessRename(source, destination string) error {
	return os.Rename(source, destination)
}

type symlinkState struct {
	exists bool
	target string
}

type profileState struct {
	path    string
	exists  bool
	mode    os.FileMode
	content []byte
}

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
		binRoot: filepath.Join(home, ".local", "bin"), shimPath: filepath.Join(home, ".local", "bin", "dsh"),
		wrapperPath: filepath.Join(finalRoot, "bin", "dsh"),
	}
}

func inspectCommandEntry(paths managedPaths, _ string) error {
	if _, err := inspectOwnedSymlink(paths.currentPath, paths.toolRoot); err != nil {
		return err
	}
	if _, err := inspectOwnedSymlink(paths.shimPath, paths.toolRoot); err != nil {
		return errExternalCommand
	}
	return nil
}

func inspectOwnedSymlink(path, root string) (symlinkState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return symlinkState{}, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return symlinkState{}, errors.New("managed command entry is not an owned symlink")
	}
	target, err := os.Readlink(path)
	if err != nil {
		return symlinkState{}, err
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(path), resolved)
	}
	if !pathWithin(root, filepath.Clean(resolved)) && filepath.Clean(resolved) != filepath.Clean(root) {
		return symlinkState{}, errors.New("managed command symlink escapes tool root")
	}
	return symlinkState{exists: true, target: target}, nil
}

func activateHarnessCommand(home string, paths managedPaths, _ string) (returnErr error) {
	currentBefore, err := inspectOwnedSymlink(paths.currentPath, paths.toolRoot)
	if err != nil {
		return err
	}
	shimBefore, err := inspectOwnedSymlink(paths.shimPath, paths.toolRoot)
	if err != nil {
		return errExternalCommand
	}
	profiles, err := captureProfiles(home)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr == nil {
			return
		}
		for index := len(profiles) - 1; index >= 0; index-- {
			_ = restoreProfile(profiles[index])
		}
		_ = restoreSymlink(paths.shimPath, shimBefore)
		_ = restoreSymlink(paths.currentPath, currentBefore)
	}()
	if err := atomicSymlink(paths.currentPath, harnessVer); err != nil {
		return err
	}
	target := filepath.Join(paths.toolRoot, "current", "bin", "dsh")
	if err := atomicSymlink(paths.shimPath, target); err != nil {
		return err
	}
	for _, profile := range profiles {
		if err := updateProfilePATH(profile); err != nil {
			return err
		}
	}
	return nil
}

func atomicSymlink(destination, target string) error {
	directory, err := os.MkdirTemp(filepath.Dir(destination), ".osverse-dsh-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	temporary := filepath.Join(directory, "link")
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	return nil
}

func restoreSymlink(path string, state symlinkState) error {
	if !state.exists {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return atomicSymlink(path, state.target)
}

func captureProfiles(home string) ([]profileState, error) {
	paths := []string{filepath.Join(home, ".profile")}
	switch filepath.Base(os.Getenv("SHELL")) {
	case "bash":
		paths = append(paths, filepath.Join(home, ".bashrc"))
	case "zsh":
		paths = append(paths, filepath.Join(home, ".zprofile"), filepath.Join(home, ".zshrc"))
	}
	states := make([]profileState, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			states = append(states, profileState{path: path, mode: 0o600})
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1024*1024 {
			return nil, errors.New("shell profile is unsafe")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		states = append(states, profileState{path: path, exists: true, mode: info.Mode().Perm(), content: content})
	}
	return states, nil
}

const (
	pathProfileStart = "# >>> Osverse user commands >>>"
	pathProfileEnd   = "# <<< Osverse user commands <<<"
	pathProfileBlock = pathProfileStart + "\ncase \":$PATH:\" in *\":$HOME/.local/bin:\"*) ;; *) export PATH=\"$HOME/.local/bin:$PATH\" ;; esac\n" + pathProfileEnd + "\n"
)

func updateProfilePATH(state profileState) error {
	startCount := bytes.Count(state.content, []byte(pathProfileStart))
	endCount := bytes.Count(state.content, []byte(pathProfileEnd))
	if startCount != endCount || startCount > 1 {
		return errors.New("shell profile contains a conflicting Osverse block")
	}
	if err := confirmProfileState(state); err != nil {
		return err
	}
	if startCount == 1 {
		return nil
	}
	content := append([]byte(nil), state.content...)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, []byte(pathProfileBlock)...)
	return atomicWriteProfile(state.path, content, state.mode)
}

func confirmProfileState(expected profileState) error {
	info, err := os.Lstat(expected.path)
	if !expected.exists {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("shell profile changed during Harness installation")
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != expected.mode.Perm() || info.Size() != int64(len(expected.content)) {
		return errors.New("shell profile changed during Harness installation")
	}
	content, err := os.ReadFile(expected.path)
	if err != nil || !bytes.Equal(content, expected.content) {
		return errors.New("shell profile changed during Harness installation")
	}
	return nil
}

func restoreProfile(state profileState) error {
	if !state.exists {
		err := os.Remove(state.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return atomicWriteProfile(state.path, state.content, state.mode)
}

func atomicWriteProfile(destination string, content []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".osverse-profile-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}
