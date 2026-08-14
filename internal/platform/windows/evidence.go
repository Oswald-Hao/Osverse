//go:build windows

package windows

// ExecutableEvidence keeps a Windows file handle open without write/delete
// sharing, preventing ordinary replacement while the caller verifies or
// starts the exact path.
type ExecutableEvidence struct {
	identity windowsFileIdentity
}

func OpenExecutableEvidence(path string) (*ExecutableEvidence, error) {
	identity, err := openLockedIdentity(path)
	if err != nil {
		return nil, err
	}
	return &ExecutableEvidence{identity: identity}, nil
}

func (evidence *ExecutableEvidence) ResolvedPath() string {
	if evidence == nil {
		return ""
	}
	return evidence.identity.finalPath
}

func (evidence *ExecutableEvidence) Close() error {
	if evidence == nil || evidence.identity.handle == 0 {
		return nil
	}
	err := closeWindowsHandle(evidence.identity.handle)
	evidence.identity.handle = 0
	return err
}

func (evidence *ExecutableEvidence) Unchanged(path string) bool {
	if evidence == nil || evidence.identity.handle == 0 {
		return false
	}
	current, err := openLockedIdentity(path)
	if err != nil {
		return false
	}
	defer closeWindowsHandle(current.handle)
	return evidence.identity.same(current) && sameWindowsPath(evidence.identity.finalPath, current.finalPath)
}
