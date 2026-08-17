//go:build linux

package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestKimiAdapterRoutesOfficialBinaryThroughExactProfile(t *testing.T) {
	binary := os.Getenv("OSVERSE_KIMI_LINUX_BINARY")
	if binary == "" {
		t.Skip("set OSVERSE_KIMI_LINUX_BINARY to the pinned official kimi binary")
	}
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer test-only-key" {
			http.NotFound(writer, request)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 2*1024*1024))
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		select {
		case requests <- decoded:
		default:
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"id\":\"osverse-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"OSVERSE_KIMI_OK\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"id\":\"osverse-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	home := t.TempDir()
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	const modelID = "deepseek/deepseek-v4-flash"
	if _, err := adapters.Apply(context.Background(), TargetKimi, Input{
		Name: "Kimi integration", APIKey: "test-only-key", BaseURL: server.URL,
		Model: modelID, AllowPrivateNetwork: true, Protocol: "openai-chat",
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--prompt", "Reply with the supplied test token.", "--output-format", "stream-json")
	command.Env = append(os.Environ(), "HOME="+home, "NO_PROXY=127.0.0.1,localhost", "NO_COLOR=1", "CI=1", "KIMI_CODE_NO_AUTO_UPDATE=1")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "OSVERSE_KIMI_OK") {
		t.Fatalf("Kimi profile run failed: %v\n%s", err, output)
	}
	select {
	case request := <-requests:
		if request["model"] != modelID {
			t.Fatalf("request model = %#v, want exact %q", request["model"], modelID)
		}
		if streaming, ok := request["stream"].(bool); !ok || !streaming {
			t.Fatalf("request stream = %#v", request["stream"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Kimi did not call the configured OpenAI Chat endpoint")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("Kimi integration timed out")
	}
}
