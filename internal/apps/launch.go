package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

type processLauncher struct{}

func (processLauncher) Start(path string) error {
	command := exec.Command(path)
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func (manager *Manager) Launch(componentID string) error {
	if manager == nil || manager.launcher == nil {
		return errors.New("application launcher unavailable")
	}
	item, ok := manager.catalog[componentID]
	if !ok {
		return ErrUnknownComponent
	}
	root := filepath.Join(manager.home, ".local", "share", "osverse", "apps", item.ID)
	current := filepath.Join(root, "current")
	resolvedCurrent, err := filepath.EvalSymlinks(current)
	if err != nil || !within(root, resolvedCurrent) {
		return errors.New("managed application unavailable")
	}
	image := filepath.Join(resolvedCurrent, "application.AppImage")
	info, err := os.Lstat(image)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("managed application is invalid")
	}
	marker, err := os.ReadFile(filepath.Join(resolvedCurrent, ".osverse-artifact-sha256"))
	if err != nil || string(marker) != item.SHA256+"\n" {
		return errors.New("managed application is invalid")
	}
	file, err := os.Open(image)
	if err != nil {
		return errors.New("managed application is invalid")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errors.New("managed application is invalid")
	}
	return manager.launcher.Start(image)
}
