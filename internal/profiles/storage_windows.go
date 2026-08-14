//go:build windows

package profiles

import (
	"errors"
	"io"
	"os"
	"unsafe"

	xwindows "golang.org/x/sys/windows"
)

const maxProtectedMasterKeyBytes = 64 * 1024

func profileStoreComponents() []string { return []string{"AppData", "Local", "Osverse", "profiles"} }

func profileBackupComponents() []string {
	return []string{"AppData", "Local", "Osverse", "profiles", "backups"}
}

func profileProtectionLabel() string { return "windows-dpapi" }

func privateProfileFileMode(os.FileInfo) bool { return true }

func loadOrCreateMasterKey(path string, random io.Reader) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxProtectedMasterKeyBytes {
			return nil, ErrUnsafeStorage
		}
		protected, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		key, err := unprotectForCurrentUser(protected)
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
	protected, err := protectForCurrentUser(key)
	if err != nil || len(protected) == 0 || len(protected) > maxProtectedMasterKeyBytes {
		return nil, ErrUnsafeStorage
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(protected); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return key, file.Close()
}

func protectForCurrentUser(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, ErrUnsafeStorage
	}
	input := xwindows.DataBlob{Size: uint32(len(plaintext)), Data: &plaintext[0]}
	var output xwindows.DataBlob
	if err := xwindows.CryptProtectData(&input, nil, nil, 0, nil, xwindows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	defer xwindows.LocalFree(xwindows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func unprotectForCurrentUser(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, ErrUnsafeStorage
	}
	input := xwindows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	var output xwindows.DataBlob
	if err := xwindows.CryptUnprotectData(&input, nil, nil, 0, nil, xwindows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	defer xwindows.LocalFree(xwindows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}
