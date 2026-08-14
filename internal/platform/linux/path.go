//go:build linux

package linux

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/unix"
)

const (
	maxPathProfileBytes  = 1 << 20
	pathProfileReadChunk = 32 * 1024
)

var pathProfileNames = []string{
	".profile",
	".bash_profile",
	".bashrc",
	".zprofile",
	".zshrc",
}

// PathInputs contains the process environment and allowlisted profile contents
// used for pure PATH candidate discovery.
type PathInputs struct {
	ProcessPath  string
	Home         string
	Shell        string
	ProfileFiles map[string][]byte
}

// DiscoverPaths returns stable, absolute PATH candidates without evaluating
// shell syntax. Profile contents are accepted only from known profile names.
func DiscoverPaths(inputs PathInputs) []string {
	paths := newPathSet()
	for _, entry := range strings.Split(inputs.ProcessPath, string(filepath.ListSeparator)) {
		paths.addAbsolute(entry)
	}

	home, homeOK := cleanAbsolute(inputs.Home)
	if homeOK {
		paths.add(filepath.Join(home, ".local", "bin"))
	}

	for _, name := range pathProfileNames {
		for _, entry := range parseProfilePaths(inputs.ProfileFiles[name], home, homeOK) {
			paths.add(entry)
		}
	}
	return paths.values
}

type pathProbe struct{}

// NewPathProbe returns the production, read-only PATH discovery adapter.
func NewPathProbe() platform.PathProbe {
	return pathProbe{}
}

func (pathProbe) Paths(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, pathScanFailed(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, pathScanFailed(err)
	}
	home, err = validatePathProbeHome(home)
	if err != nil {
		return nil, pathScanFailed(err)
	}
	homeFD, err := openPathProbeHome(home)
	if err != nil {
		return nil, pathScanFailed(err)
	}
	defer unix.Close(homeFD)

	profiles, err := readPathProfiles(ctx, homeFD)
	if err != nil {
		return nil, pathScanFailed(err)
	}

	return DiscoverPaths(PathInputs{
		ProcessPath:  os.Getenv("PATH"),
		Home:         home,
		Shell:        os.Getenv("SHELL"),
		ProfileFiles: profiles,
	}), nil
}

func validatePathProbeHome(home string) (string, error) {
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return "", errors.New("invalid user home")
	}
	return home, nil
}

func openPathProbeHome(home string) (int, error) {
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, errors.New("user home is not a directory")
	}
	return fd, nil
}

func readPathProfiles(ctx context.Context, homeFD int) (map[string][]byte, error) {
	profiles := make(map[string][]byte, len(pathProfileNames))
	for _, name := range pathProfileNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contents, err := readPathProfile(ctx, homeFD, name)
		if err == nil {
			profiles[name] = contents
			continue
		}
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return nil, err
	}
	return profiles, nil
}

func readPathProfile(ctx context.Context, homeFD int, name string) ([]byte, error) {
	fd, err := unix.Openat(homeFD, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open profile file")
	}
	defer file.Close()

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("profile is not a regular file")
	}
	if info.Size > maxPathProfileBytes {
		return nil, errors.New("profile exceeds size limit")
	}
	return readBoundedPathProfile(ctx, file)
}

func readBoundedPathProfile(ctx context.Context, reader io.Reader) ([]byte, error) {
	contents := make([]byte, 0, pathProfileReadChunk)
	buffer := make([]byte, pathProfileReadChunk)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		count, err := reader.Read(buffer)
		if count > 0 {
			if count > maxPathProfileBytes-len(contents) {
				return nil, errors.New("profile exceeds size limit")
			}
			contents = append(contents, buffer[:count]...)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err == io.EOF {
			return contents, nil
		}
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, io.ErrNoProgress
		}
	}
}

func pathScanFailed(cause error) error {
	return domain.NewPublicError(domain.ErrScanFailed, "path discovery failed", cause)
}

type pathSet struct {
	values []string
	seen   map[string]struct{}
}

func newPathSet() *pathSet {
	return &pathSet{seen: make(map[string]struct{})}
}

func (paths *pathSet) addAbsolute(value string) {
	cleaned, ok := cleanAbsolute(value)
	if ok {
		paths.add(cleaned)
	}
}

func (paths *pathSet) add(value string) {
	if _, present := paths.seen[value]; present {
		return
	}
	paths.seen[value] = struct{}{}
	paths.values = append(paths.values, value)
}

func cleanAbsolute(value string) (string, bool) {
	if value == "" || !filepath.IsAbs(value) {
		return "", false
	}
	return filepath.Clean(value), true
}

func parseProfilePaths(contents []byte, home string, homeOK bool) []string {
	var paths []string
	for _, line := range strings.Split(string(contents), "\n") {
		value, ok := pathAssignment(line)
		if !ok {
			continue
		}
		entries, ok := parsePathAssignment(value, home, homeOK)
		if ok {
			paths = append(paths, entries...)
		}
	}
	return paths
}

func pathAssignment(line string) (string, bool) {
	line = strings.TrimSpace(line)
	var value string
	switch {
	case strings.HasPrefix(line, "PATH="):
		value = line[len("PATH="):]
	case strings.HasPrefix(line, "export"):
		rest := line[len("export"):]
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			return "", false
		}
		rest = strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(rest, "PATH=") {
			return "", false
		}
		value = rest[len("PATH="):]
	default:
		return "", false
	}

	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	} else if strings.ContainsAny(value, "\"'") {
		return "", false
	}
	return value, true
}

func parsePathAssignment(value, home string, homeOK bool) ([]string, bool) {
	segments := strings.Split(value, string(filepath.ListSeparator))
	paths := make([]string, 0, len(segments))
	for _, segment := range segments {
		path, pathReference, ok := parsePathSegment(segment, home, homeOK)
		if !ok {
			return nil, false
		}
		if !pathReference {
			paths = append(paths, path)
		}
	}
	return paths, true
}

func parsePathSegment(segment, home string, homeOK bool) (string, bool, bool) {
	if segment == "$PATH" || segment == "${PATH}" {
		return "", true, true
	}
	if segment == "" || strings.IndexFunc(segment, unicode.IsSpace) >= 0 || strings.ContainsAny(segment, "`;&|<>()*?[]\\'\"!#\x00") {
		return "", false, false
	}

	var expanded strings.Builder
	for index := 0; index < len(segment); {
		if segment[index] != '$' {
			expanded.WriteByte(segment[index])
			index++
			continue
		}

		if strings.HasPrefix(segment[index:], "${HOME}") {
			if !homeOK {
				return "", false, false
			}
			expanded.WriteString(home)
			index += len("${HOME}")
			continue
		}
		if strings.HasPrefix(segment[index:], "${PATH}") {
			return "", false, false
		}
		if strings.HasPrefix(segment[index:], "$HOME") {
			next := index + len("$HOME")
			if !homeOK || (next < len(segment) && isShellNameCharacter(rune(segment[next]))) {
				return "", false, false
			}
			expanded.WriteString(home)
			index = next
			continue
		}
		if strings.HasPrefix(segment[index:], "$PATH") {
			return "", false, false
		}
		return "", false, false
	}

	path, ok := cleanAbsolute(expanded.String())
	return path, false, ok
}

func isShellNameCharacter(value rune) bool {
	return value == '_' || unicode.IsLetter(value) || unicode.IsDigit(value)
}
