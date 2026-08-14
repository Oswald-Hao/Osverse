package systeminstall

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

type fakeProbe struct {
	info domain.SystemInfo
	err  error
}

func (probe fakeProbe) Probe(context.Context) (domain.SystemInfo, error) {
	return probe.info, probe.err
}

type fakeRunner struct {
	mu       sync.Mutex
	requests []platform.CommandRequest
	result   platform.CommandResult
	err      error
}

func (runner *fakeRunner) Run(_ context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
	runner.mu.Lock()
	runner.requests = append(runner.requests, request)
	runner.mu.Unlock()
	return runner.result, runner.err
}

func TestClaudePlanRequiresSupportedUbuntuAndListsFixedEffects(t *testing.T) {
	manager := testSystemManager()
	manager.probe = fakeProbe{info: domain.SystemInfo{Version: "22.04", Architecture: "x86_64", Supported: true}}
	plan, err := manager.CreatePlan(context.Background(), componentID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ComponentID != componentID || plan.Version != "APT stable" || len(plan.Changes) != 4 {
		t.Fatalf("plan = %#v", plan)
	}
	wantPaths := []string{"/usr/bin/pkexec", "/usr/share/keyrings/claude-desktop-archive-keyring.asc", "/etc/apt/sources.list.d/claude-desktop.list", "claude-desktop"}
	for index, want := range wantPaths {
		if plan.Changes[index].Path != want {
			t.Fatalf("change %d = %#v", index, plan.Changes[index])
		}
	}
	manager.probe = fakeProbe{info: domain.SystemInfo{Version: "20.04", Architecture: "x86_64", Supported: true}}
	if _, err := manager.CreatePlan(context.Background(), componentID); err != ErrUnsupportedTarget {
		t.Fatalf("Ubuntu 20.04 = %v", err)
	}
	if _, err := manager.CreatePlan(context.Background(), "other"); err != ErrUnknownComponent {
		t.Fatalf("unknown = %v", err)
	}
}

func TestSystemTaskInvokesOnlyExactPrivilegedHelper(t *testing.T) {
	manager := testSystemManager()
	manager.probe = fakeProbe{info: domain.SystemInfo{Version: "22.04", Architecture: "x86_64", Supported: true}}
	runner := &fakeRunner{result: platform.CommandResult{ExitCode: 0}}
	manager.runner = runner
	plan, _ := manager.CreatePlan(context.Background(), componentID)
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for task.Phase != "completed" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		task, err = manager.Task(task.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if task.Phase != "completed" {
		t.Fatalf("task = %#v", task)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.requests) != 1 {
		t.Fatalf("requests = %d", len(runner.requests))
	}
	request := runner.requests[0]
	if request.Path != "/usr/bin/pkexec" || !reflect.DeepEqual(request.Args, []string{manager.executable, privilegedFlag, privilegedAction}) {
		t.Fatalf("request = %#v", request)
	}
	if _, err := manager.Start(context.Background(), plan.ID, "", 0); err != ErrPlanUnavailable {
		t.Fatalf("reused plan = %v", err)
	}
}

func TestSystemRemoveInvokesOnlyExactPrivilegedPackageHelper(t *testing.T) {
	manager := testSystemManager()
	runner := &fakeRunner{result: platform.CommandResult{ExitCode: 0}}
	manager.runner = runner
	if err := manager.Remove(context.Background(), "claude-desktop"); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.requests) != 1 {
		t.Fatalf("requests = %d", len(runner.requests))
	}
	want := []string{manager.executable, privilegedFlag, privilegedRemoveAction, "claude-desktop"}
	if request := runner.requests[0]; request.Path != "/usr/bin/pkexec" || !reflect.DeepEqual(request.Args, want) {
		t.Fatalf("remove request = %#v", request)
	}
	if err := manager.Remove(context.Background(), "unknown"); !errors.Is(err, ErrUnknownComponent) {
		t.Fatalf("unknown removal error = %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatal("unknown component reached privileged runner")
	}
}

func TestPrivilegedPackageRemovalUsesFixedAptPackage(t *testing.T) {
	var path string
	var args []string
	deps := privilegedDeps{root: "/", run: func(_ context.Context, command string, commandArgs []string, _ []byte) ([]byte, error) {
		path, args = command, append([]string(nil), commandArgs...)
		return nil, nil
	}}
	if err := removeSystemPackage(context.Background(), deps, "cockpit-tools"); err != nil {
		t.Fatal(err)
	}
	if path != "/usr/bin/apt-get" || !reflect.DeepEqual(args, []string{"remove", "-y", "cockpit-tools"}) {
		t.Fatalf("apt removal = %q %#v", path, args)
	}
	if err := removeSystemPackage(context.Background(), deps, "unknown"); !errors.Is(err, ErrUnknownComponent) {
		t.Fatalf("unknown package error = %v", err)
	}
}

func TestAPTProxyOptionsAreLoopbackOnly(t *testing.T) {
	if got := aptProxyOptions("", 0); got != nil {
		t.Fatalf("direct options = %#v", got)
	}
	wantHTTP := []string{"-o", "Acquire::http::Proxy=http://127.0.0.1:7890", "-o", "Acquire::https::Proxy=http://127.0.0.1:7890"}
	if got := aptProxyOptions("http", 7890); !reflect.DeepEqual(got, wantHTTP) {
		t.Fatalf("HTTP options = %#v", got)
	}
	wantSOCKS := []string{"-o", "Acquire::http::Proxy=socks5h://127.0.0.1:1080", "-o", "Acquire::https::Proxy=socks5h://127.0.0.1:1080"}
	if got := aptProxyOptions("socks5", 1080); !reflect.DeepEqual(got, wantSOCKS) {
		t.Fatalf("SOCKS options = %#v", got)
	}
}

func testSystemManager() *Manager {
	sequence := 0
	return &Manager{
		now:        func() time.Time { return time.Unix(1000, 0) },
		randomID:   func() (string, error) { sequence++; return "system-test-" + string(rune('0'+sequence)), nil },
		executable: "/opt/osverse/Osverse", plans: map[string]*planState{}, tasks: map[string]*taskState{},
	}
}

type staticTransport struct {
	body   string
	status int
}

func (transport staticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: transport.status, ContentLength: int64(len(transport.body)), Body: ioNopCloser{strings.NewReader(transport.body)}, Header: http.Header{}, Request: request}, nil
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }

func TestPrivilegedInstallVerifiesKeyAndRollsBackOwnedFiles(t *testing.T) {
	root := t.TempDir()
	prepareSystemRoot(t, root, "22.04")
	var commands []string
	run := func(_ context.Context, path string, args []string, stdin []byte) ([]byte, error) {
		commands = append(commands, path+" "+strings.Join(args, " "))
		if path == "/usr/bin/gpg" {
			if string(stdin) != "public-key" {
				t.Fatalf("gpg stdin = %q", stdin)
			}
			return []byte("fpr:::::::::" + expectedFingerprint + ":\n"), nil
		}
		return nil, nil
	}
	deps := privilegedDeps{root: root, client: &http.Client{Transport: staticTransport{body: "public-key", status: 200}}, run: run}
	if err := installClaude(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(root, "usr/share/keyrings/claude-desktop-archive-keyring.asc"))
	source, _ := os.ReadFile(filepath.Join(root, "etc/apt/sources.list.d/claude-desktop.list"))
	if string(key) != "public-key" || string(source) != repositoryLine {
		t.Fatalf("installed key/source = %q / %q", key, source)
	}
	wantCommands := []string{
		"/usr/bin/gpg --batch --with-colons --import-options show-only --import",
		"/usr/bin/apt-get update", "/usr/bin/apt-get install -y claude-desktop",
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v", commands)
	}

	failedRoot := t.TempDir()
	prepareSystemRoot(t, failedRoot, "22.04")
	failing := deps
	failing.root = failedRoot
	failing.run = func(_ context.Context, path string, _ []string, _ []byte) ([]byte, error) {
		if path == "/usr/bin/gpg" {
			return []byte("fpr:::::::::" + expectedFingerprint + ":\n"), nil
		}
		return nil, errors.New("apt failed")
	}
	if err := installClaude(context.Background(), failing); err == nil {
		t.Fatal("apt failure accepted")
	}
	for _, path := range []string{"usr/share/keyrings/claude-desktop-archive-keyring.asc", "etc/apt/sources.list.d/claude-desktop.list"} {
		if _, err := os.Lstat(filepath.Join(failedRoot, path)); !os.IsNotExist(err) {
			t.Fatalf("rollback left %s: %v", path, err)
		}
	}
}

func TestPrivilegedInstallRejectsWrongFingerprintAndExternalSource(t *testing.T) {
	root := t.TempDir()
	prepareSystemRoot(t, root, "22.04")
	deps := privilegedDeps{
		root: root, client: &http.Client{Transport: staticTransport{body: "wrong", status: 200}},
		run: func(context.Context, string, []string, []byte) ([]byte, error) {
			return []byte("fpr:::::::::BAD:\n"), nil
		},
	}
	if err := installClaude(context.Background(), deps); err == nil {
		t.Fatal("wrong fingerprint accepted")
	}
	sourcePath := filepath.Join(root, "etc/apt/sources.list.d/claude-desktop.list")
	if err := os.WriteFile(sourcePath, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps.run = func(_ context.Context, path string, _ []string, _ []byte) ([]byte, error) {
		if path == "/usr/bin/gpg" {
			return []byte("fpr:::::::::" + expectedFingerprint + ":\n"), nil
		}
		return nil, nil
	}
	if err := installClaude(context.Background(), deps); err != ErrExternalEntry {
		t.Fatalf("external source = %v", err)
	}
	content, _ := os.ReadFile(sourcePath)
	if string(content) != "external\n" {
		t.Fatalf("external source changed: %q", content)
	}
}

func prepareSystemRoot(t *testing.T, root, version string) {
	t.Helper()
	for _, directory := range []string{"etc", "etc/apt/sources.list.d", "usr/share/keyrings"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("ID=ubuntu\nVERSION_ID=\""+version+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
