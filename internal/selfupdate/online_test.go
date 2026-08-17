package selfupdate

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

// This opt-in test exercises the exact unauthenticated release feed and
// manifest path used by production without placing network access in CI.
func TestOnlinePublishedReleaseCheck(t *testing.T) {
	portText := os.Getenv("OSVERSE_TEST_ONLINE_PROXY_PORT")
	if portText == "" {
		t.Skip("set OSVERSE_TEST_ONLINE_PROXY_PORT to run the live update check")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("invalid proxy port %q", portText)
	}
	service := NewService(t.TempDir(), "0.7.1-beta.1")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	info, err := service.Check(ctx, proxyservice.ProtocolHTTPSConnect, port)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.LatestVersion == "" || info.ReleaseName == "" || info.ReleaseNotes == "" {
		t.Fatalf("incomplete online update info: %#v", info)
	}
}
