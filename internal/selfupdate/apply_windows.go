//go:build windows

package selfupdate

import (
	"errors"
	"os/exec"
)

func applyArtifact(staged string, artifact Artifact) (ApplyResult, error) {
	if artifact.Format != "nsis" {
		return ApplyResult{}, errors.New("unsupported Windows update format")
	}
	process := exec.Command(staged)
	if err := process.Start(); err != nil {
		return ApplyResult{}, err
	}
	if process.Process != nil {
		_ = process.Process.Release()
	}
	return ApplyResult{Started: true, ShouldQuit: true, Message: "更新安装程序已启动，Osverse 即将退出"}, nil
}
