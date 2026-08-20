//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	xwindows "golang.org/x/sys/windows"
)

// ErrMoveInUse means Windows rejected the final atomic rename because the
// already-pinned source object, or a child beneath it, still has an open handle.
var ErrMoveInUse = errors.New("movable path is in use")

// MovableEvidence pins one non-reparse file or directory and can rename that
// exact object without reopening its source path.
type MovableEvidence struct {
	identity windowsFileIdentity
}

func OpenMovableEvidence(path string) (*MovableEvidence, error) {
	if !safeExecutablePath(path) {
		return nil, errors.New("movable path is unsafe")
	}
	name, err := xwindows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := xwindows.CreateFile(name, xwindows.DELETE|xwindows.FILE_READ_ATTRIBUTES, xwindows.FILE_SHARE_READ, nil,
		xwindows.OPEN_EXISTING, xwindows.FILE_FLAG_BACKUP_SEMANTICS|xwindows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = xwindows.CloseHandle(handle)
		}
	}()
	var info xwindows.ByHandleFileInformation
	if err := xwindows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&xwindows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, errors.New("movable path is a reparse point")
	}
	finalPath, err := finalPathForHandle(handle)
	if err != nil {
		return nil, errors.New("movable path identity unavailable")
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil || !sameWindowsPath(resolvedPath, finalPath) {
		return nil, errors.New("movable path identity unavailable")
	}
	closeOnError = false
	return &MovableEvidence{identity: windowsFileIdentity{
		handle: handle, finalPath: finalPath, volumeSerial: info.VolumeSerialNumber,
		fileIndex: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		fileSize:  uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow), lastWrite: info.LastWriteTime,
	}}, nil
}

func (evidence *MovableEvidence) Path() string {
	if evidence == nil {
		return ""
	}
	return evidence.identity.finalPath
}

func (evidence *MovableEvidence) Close() error {
	if evidence == nil || evidence.identity.handle == 0 {
		return nil
	}
	err := xwindows.CloseHandle(evidence.identity.handle)
	evidence.identity.handle = 0
	return err
}

type fileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   xwindows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func (evidence *MovableEvidence) MoveTo(destination string) error {
	if evidence == nil || evidence.identity.handle == 0 || !safeExecutablePath(destination) ||
		strings.EqualFold(filepath.Clean(destination), filepath.Clean(evidence.identity.finalPath)) {
		return errors.New("move destination is unsafe")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("move destination already exists")
	}
	parent := filepath.Dir(destination)
	parentName, err := xwindows.UTF16PtrFromString(parent)
	if err != nil {
		return err
	}
	parentHandle, err := xwindows.CreateFile(parentName, xwindows.FILE_TRAVERSE, xwindows.FILE_SHARE_READ|xwindows.FILE_SHARE_WRITE|xwindows.FILE_SHARE_DELETE,
		nil, xwindows.OPEN_EXISTING, xwindows.FILE_FLAG_BACKUP_SEMANTICS|xwindows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer xwindows.CloseHandle(parentHandle)
	var parentInfo xwindows.ByHandleFileInformation
	if err := xwindows.GetFileInformationByHandle(parentHandle, &parentInfo); err != nil ||
		parentInfo.FileAttributes&xwindows.FILE_ATTRIBUTE_DIRECTORY == 0 || parentInfo.FileAttributes&xwindows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("move destination directory is unsafe")
	}
	name, err := xwindows.UTF16FromString(filepath.Base(destination))
	if err != nil {
		return err
	}
	name = name[:len(name)-1]
	var layout fileRenameInformation
	bufferSize := int(unsafe.Offsetof(layout.FileName)) + len(name)*2
	buffer := make([]byte, bufferSize)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = parentHandle
	info.FileNameLength = uint32(len(name) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	var status xwindows.IO_STATUS_BLOCK
	if err := xwindows.NtSetInformationFile(evidence.identity.handle, &status, &buffer[0], uint32(len(buffer)), xwindows.FileRenameInformation); err != nil {
		return classifyRenameError(err)
	}
	evidence.identity.finalPath = filepath.Clean(destination)
	return nil
}

func classifyRenameError(err error) error {
	var status xwindows.NTStatus
	if !errors.As(err, &status) {
		return err
	}
	switch status {
	case xwindows.STATUS_ACCESS_DENIED,
		xwindows.STATUS_SHARING_VIOLATION,
		xwindows.STATUS_FILE_LOCK_CONFLICT,
		xwindows.STATUS_LOCK_NOT_GRANTED:
		return fmt.Errorf("%w: %v", ErrMoveInUse, err)
	default:
		return err
	}
}
