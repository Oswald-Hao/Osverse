package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const harnessE2EMarker = "OSVERSE_HARNESS_E2E_OK"

// This opt-in test boots the exact installed Harness release with files
// produced by the Go adapter. CI keeps the hermetic suite; release validation
// can point OSVERSE_REAL_HARNESS_COMMAND at a verified dsh 0.1.0-rc.6 command.
func TestHarnessAdapterConfigBootsPinnedHarness(t *testing.T) {
	commandPath := pinnedHarnessCommand(t)

	home := t.TempDir()
	dshHome := filepath.Join(home, ".dsh")
	t.Setenv("DSH_HOME", dshHome)
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	input := adapterInput()
	input.Protocol = "openai-chat"
	if _, err := adapters.Apply(context.Background(), TargetHarness, input); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	command := exec.CommandContext(ctx, commandPath, "web", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	command.Dir = home
	command.Env = append(os.Environ(), "DSH_HOME="+dshHome)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	for {
		select {
		case runErr := <-done:
			cancel()
			t.Fatalf("Harness exited before serving: %v\n%s", runErr, output.String())
		case <-ctx.Done():
			<-done
			t.Fatalf("Harness did not accept generated config: %v\n%s", ctx.Err(), output.String())
		default:
		}
		response, requestErr := client.Get(endpoint)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				cancel()
				runErr := <-done
				if runErr != nil && !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatalf("Harness shutdown error: %v", runErr)
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestHarnessAdapterRoutesPinnedHarnessRequestThroughProfile(t *testing.T) {
	commandPath := pinnedHarnessCommand(t)
	requestSeen := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(response, request)
			return
		}
		var payload struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		err := json.NewDecoder(request.Body).Decode(&payload)
		if err == nil && request.Header.Get("Authorization") != "Bearer secret-key-1234" {
			err = errors.New("Harness did not resolve OSVERSE_API_KEY into the authorization header")
		}
		if err == nil && payload.Model != "deepseek/deepseek-v4-flash" {
			err = errors.New("Harness changed the configured model id")
		}
		if err == nil && !payload.Stream {
			err = errors.New("Harness request was unexpectedly non-streaming")
		}
		select {
		case requestSeen <- err:
		default:
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("data: {\"id\":\"chatcmpl-osverse\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"deepseek/deepseek-v4-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"" + harnessE2EMarker + "\"},\"finish_reason\":null}]}\n\n"))
		_, _ = response.Write([]byte("data: {\"id\":\"chatcmpl-osverse\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"deepseek/deepseek-v4-flash\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = response.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	home := t.TempDir()
	dshHome := filepath.Join(home, ".dsh")
	t.Setenv("DSH_HOME", dshHome)
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	input := adapterInput()
	input.Name = "Harness E2E"
	// Production users commonly paste the gateway origin. The adapter must
	// version OpenAI-compatible URLs before Harness appends chat/completions.
	input.BaseURL = server.URL
	input.Model = "deepseek/deepseek-v4-flash"
	input.AllowPrivateNetwork = true
	input.Protocol = "openai-chat"
	if _, err := adapters.Apply(context.Background(), TargetHarness, input); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, commandPath, "--profile", "headless", "reply with the configured marker")
	command.Dir = home
	command.Env = append(os.Environ(), "DSH_HOME="+dshHome)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Harness headless request failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), harnessE2EMarker) {
		t.Fatalf("Harness did not return the mock provider response:\n%s", output)
	}
	select {
	case requestErr := <-requestSeen:
		if requestErr != nil {
			t.Fatal(requestErr)
		}
	default:
		t.Fatal("Harness never called the configured provider")
	}
}

func pinnedHarnessCommand(t *testing.T) string {
	t.Helper()
	commandPath := os.Getenv("OSVERSE_REAL_HARNESS_COMMAND")
	if commandPath == "" {
		t.Skip("set OSVERSE_REAL_HARNESS_COMMAND to the pinned dsh command")
	}
	versionCommand := exec.Command(commandPath, "--version")
	version, err := versionCommand.Output()
	if err != nil || strings.TrimSpace(string(version)) != "0.1.0-rc.6" {
		t.Fatalf("unexpected Harness command: version=%q err=%v", version, err)
	}
	return commandPath
}
