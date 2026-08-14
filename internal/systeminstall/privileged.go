//go:build linux

package systeminstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const (
	privilegedFlag         = "--osverse-privileged"
	privilegedAction       = "install-claude-desktop"
	privilegedRemoveAction = "remove-system-package"
	keyURL                 = "https://downloads.claude.ai/claude-desktop/key.asc"
	expectedFingerprint    = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"
	repositoryLine         = "deb [signed-by=/usr/share/keyrings/claude-desktop-archive-keyring.asc] https://downloads.claude.ai/claude-desktop/apt/stable stable main\n"
	maxKeyBytes            = 1024 * 1024
)

type privilegedDeps struct {
	root       string
	client     *http.Client
	run        func(context.Context, string, []string, []byte) ([]byte, error)
	aptOptions []string
}

// IsPrivilegedInvocation recognizes the exact private helper entry point.
func IsPrivilegedInvocation(args []string) bool {
	return len(args) > 0 && args[0] == privilegedFlag
}

// RunPrivileged returns a process exit code and accepts no user-controlled operation.
func RunPrivileged(args []string) int {
	if len(args) < 2 || args[0] != privilegedFlag || os.Geteuid() != 0 {
		return 2
	}
	if args[1] == privilegedRemoveAction {
		if len(args) != 3 {
			return 2
		}
		deps := privilegedDeps{root: "/", run: runFixedCommand}
		if err := removeSystemPackage(context.Background(), deps, args[2]); err != nil {
			return 1
		}
		return 0
	}
	if (len(args) != 2 && len(args) != 4) || args[1] != privilegedAction {
		return 2
	}
	protocol, port := proxyservice.Protocol(""), 0
	if len(args) == 4 {
		protocol = proxyservice.Protocol(args[2])
		parsedPort, err := strconv.Atoi(args[3])
		if err != nil {
			return 2
		}
		port = parsedPort
	}
	client, err := proxyservice.NewHTTPClient(protocol, port)
	if err != nil {
		return 2
	}
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect disabled") }
	deps := privilegedDeps{root: "/", client: client, run: runFixedCommand, aptOptions: aptProxyOptions(protocol, port)}
	if err := installClaude(context.Background(), deps); err != nil {
		return 1
	}
	return 0
}

var removableSystemPackages = map[string]string{
	"claude-desktop":   "claude-desktop",
	"chatgpt-desktop":  "chatgpt-desktop",
	"opencode-desktop": "opencode-desktop",
	"cc-switch":        "cc-switch",
	"cockpit-tools":    "cockpit-tools",
}

func removeSystemPackage(ctx context.Context, deps privilegedDeps, componentID string) error {
	packageName, ok := removableSystemPackages[componentID]
	if !ok {
		return ErrUnknownComponent
	}
	if deps.root != "/" || deps.run == nil {
		return errors.New("privileged dependencies unavailable")
	}
	_, err := deps.run(ctx, "/usr/bin/apt-get", []string{"remove", "-y", packageName}, nil)
	return err
}

func installClaude(ctx context.Context, deps privilegedDeps) (returnErr error) {
	if deps.root == "" || deps.client == nil || deps.run == nil {
		return errors.New("privileged dependencies unavailable")
	}
	osRelease, err := os.ReadFile(filepath.Join(deps.root, "etc", "os-release"))
	if err != nil || !supportedClaudeOS(osRelease) {
		return ErrUnsupportedTarget
	}
	keyPath := filepath.Join(deps.root, "usr", "share", "keyrings", "claude-desktop-archive-keyring.asc")
	sourcePath := filepath.Join(deps.root, "etc", "apt", "sources.list.d", "claude-desktop.list")
	keyCreated, sourceCreated := false, false
	defer func() {
		if returnErr == nil {
			return
		}
		if sourceCreated {
			_ = os.Remove(sourcePath)
		}
		if keyCreated {
			_ = os.Remove(keyPath)
		}
	}()

	key, exists, err := readRegularBounded(keyPath, maxKeyBytes)
	if err != nil {
		return err
	}
	if !exists {
		key, err = downloadKey(ctx, deps.client)
		if err != nil {
			return err
		}
	}
	if err := verifyFingerprint(ctx, deps.run, key); err != nil {
		return err
	}
	if !exists {
		if err := createSystemFile(keyPath, key, 0o644); err != nil {
			return err
		}
		keyCreated = true
	}
	source, sourceExists, err := readRegularBounded(sourcePath, 4096)
	if err != nil {
		return err
	}
	if sourceExists && string(source) != repositoryLine {
		return ErrExternalEntry
	}
	if !sourceExists {
		if err := createSystemFile(sourcePath, []byte(repositoryLine), 0o644); err != nil {
			return err
		}
		sourceCreated = true
	}
	if _, err := deps.run(ctx, "/usr/bin/apt-get", append(append([]string{}, deps.aptOptions...), "update"), nil); err != nil {
		return err
	}
	if _, err := deps.run(ctx, "/usr/bin/apt-get", append(append([]string{}, deps.aptOptions...), "install", "-y", "claude-desktop"), nil); err != nil {
		return err
	}
	return nil
}

func aptProxyOptions(protocol proxyservice.Protocol, port int) []string {
	if protocol == "" {
		return nil
	}
	scheme := "http"
	if protocol == proxyservice.ProtocolSOCKS5 {
		scheme = "socks5h"
	}
	proxy := scheme + "://127.0.0.1:" + strconv.Itoa(port)
	return []string{"-o", "Acquire::http::Proxy=" + proxy, "-o", "Acquire::https::Proxy=" + proxy}
}

func supportedClaudeOS(data []byte) bool {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (key != "ID" && key != "VERSION_ID") {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values["ID"] == "ubuntu" && values["VERSION_ID"] == "22.04"
}

func downloadKey(ctx context.Context, client *http.Client) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, keyURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-System-Installer")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxKeyBytes {
		return nil, errors.New("key download failed")
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, maxKeyBytes+1))
	if err != nil || len(value) == 0 || len(value) > maxKeyBytes {
		return nil, errors.New("key download failed")
	}
	return value, nil
}

func verifyFingerprint(ctx context.Context, run func(context.Context, string, []string, []byte) ([]byte, error), key []byte) error {
	output, err := run(ctx, "/usr/bin/gpg", []string{"--batch", "--with-colons", "--import-options", "show-only", "--import"}, key)
	if err != nil {
		return errors.New("signing key verification failed")
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" && fields[9] == expectedFingerprint {
			return nil
		}
	}
	return errors.New("signing key fingerprint mismatch")
}

func readRegularBounded(path string, limit int64) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return nil, false, ErrExternalEntry
	}
	value, err := os.ReadFile(path)
	return value, true, err
}

func createSystemFile(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".osverse-system-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(name, path); err != nil {
		return err
	}
	return os.Remove(name)
}

func runFixedCommand(ctx context.Context, path string, args []string, stdin []byte) ([]byte, error) {
	if path != "/usr/bin/gpg" && path != "/usr/bin/apt-get" {
		return nil, errors.New("command not allowed")
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive"}
	command.Stdin = bytes.NewReader(stdin)
	var output bytes.Buffer
	if path == "/usr/bin/gpg" {
		command.Stdout = &output
	}
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	if output.Len() > 128*1024 {
		return nil, fmt.Errorf("command output too large")
	}
	return output.Bytes(), nil
}
