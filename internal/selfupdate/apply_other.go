//go:build !linux && !windows && !darwin

package selfupdate

import "errors"

func applyArtifact(string, Artifact) (ApplyResult, error) {
	return ApplyResult{}, errors.New("automatic update is unsupported on this platform")
}
