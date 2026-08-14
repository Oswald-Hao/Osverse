package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	profileLimit     = 1024 * 1024
	pathBlockStart   = "# >>> osverse managed PATH >>>"
	pathBlockEnd     = "# <<< osverse managed PATH <<<"
	managedPATHBlock = `# >>> osverse managed PATH >>>
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
# <<< osverse managed PATH <<<`
)

func ensureProfilePATH(profilePath, backupPath string) (profileState, error) {
	state := profileState{path: profilePath, mode: 0o600}
	info, err := os.Lstat(profilePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return profileState{}, err
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular():
		return profileState{}, errors.New("shell profile is not a regular file")
	default:
		if info.Size() > profileLimit {
			return profileState{}, errors.New("shell profile exceeds size limit")
		}
		state.exists = true
		state.mode = info.Mode().Perm()
		state.content, err = os.ReadFile(profilePath)
		if err != nil {
			return profileState{}, err
		}
	}
	startCount := bytes.Count(state.content, []byte(pathBlockStart))
	endCount := bytes.Count(state.content, []byte(pathBlockEnd))
	if startCount != endCount || startCount > 1 {
		return profileState{}, errors.New("shell profile contains a conflicting Osverse block")
	}
	if startCount == 1 || profileAlreadyHasLocalBin(state.content) {
		return state, nil
	}
	next := append([]byte(nil), state.content...)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	if len(next) > 0 {
		next = append(next, '\n')
	}
	next = append(next, managedPATHBlock...)
	next = append(next, '\n')
	if len(next) > profileLimit {
		return profileState{}, errors.New("shell profile update exceeds size limit")
	}
	if err := ensureProfileBackup(backupPath, state.content); err != nil {
		return profileState{}, err
	}
	if err := atomicWriteProfile(profilePath, next, state.mode); err != nil {
		return profileState{}, err
	}
	state.changed = true
	return state, nil
}

func ensureProfileBackup(backupPath string, content []byte) error {
	info, err := os.Lstat(backupPath)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return errors.New("shell profile backup path is unsafe")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
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
	return file.Close()
}

func profileAlreadyHasLocalBin(content []byte) bool {
	return bytes.Contains(content, []byte("$HOME/.local/bin")) ||
		bytes.Contains(content, []byte("${HOME}/.local/bin"))
}

func restoreProfile(state profileState) error {
	if !state.changed {
		return nil
	}
	if !state.exists {
		info, err := os.Lstat(state.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		return os.Remove(state.path)
	}
	return atomicWriteProfile(state.path, state.content, state.mode)
}

func atomicWriteProfile(profilePath string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(profilePath)
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("shell profile directory is unsafe")
	}
	temporary := filepath.Join(directory, ".osverse-profile-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, profilePath); err != nil {
		return fmt.Errorf("replace shell profile: %w", err)
	}
	cleanup = false
	return nil
}
