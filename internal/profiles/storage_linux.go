//go:build linux

package profiles

import (
	"errors"
	"io"
	"os"
)

func profileStoreComponents() []string { return []string{".local", "share", "osverse", "profiles"} }

func profileBackupComponents() []string {
	return []string{".local", "share", "osverse", "profiles", "backups"}
}

func profileProtectionLabel() string { return "local-file" }

func privateProfileFileMode(info os.FileInfo) bool { return info.Mode().Perm()&0o077 == 0 }

func loadOrCreateMasterKey(path string, random io.Reader) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() != 32 {
			return nil, ErrUnsafeStorage
		}
		key, err := os.ReadFile(path)
		if err != nil || len(key) != 32 {
			return nil, ErrUnsafeStorage
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(random, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return key, file.Close()
}
