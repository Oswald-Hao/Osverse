//go:build windows

package windows

import "os"

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

// TakeFile transfers ownership of the pinned Windows handle to an os.File.
// The evidence becomes inert, so its Close method remains safe to call.
func (evidence *ExecutableEvidence) TakeFile() *os.File {
	if evidence == nil || evidence.identity.handle == 0 {
		return nil
	}
	handle := evidence.identity.handle
	name := evidence.identity.finalPath
	evidence.identity.handle = 0
	file := os.NewFile(uintptr(handle), name)
	if file == nil {
		_ = closeWindowsHandle(handle)
	}
	return file
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
