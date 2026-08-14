//go:build darwin

package selfupdate

import (
	"errors"
	"os/exec"
)

func applyArtifact(staged string, artifact Artifact) (ApplyResult, error) {
	if artifact.Format != "dmg" {
		return ApplyResult{}, errors.New("unsupported macOS update format")
	}
	process := exec.Command("/usr/bin/open", staged)
	if err := process.Start(); err != nil {
		return ApplyResult{}, err
	}
	if process.Process != nil {
		_ = process.Process.Release()
	}
	return ApplyResult{Started: true, Message: "更新磁盘映像已打开，请确认替换 Osverse"}, nil
}
