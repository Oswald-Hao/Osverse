package proxy

import (
	"net/http"
	"testing"
)

func TestHTTPClientUsesOnlyDirectOrLoopbackProxyConfiguration(t *testing.T) {
	direct, err := NewHTTPClient("", 0)
	if err != nil {
		t.Fatal(err)
	}
	directTransport := direct.Transport.(*http.Transport)
	if directTransport.Proxy != nil {
		t.Fatal("direct client inherited environment proxy")
	}

	connect, err := NewHTTPClient(ProtocolHTTPSConnect, 7890)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://registry.npmjs.org/package", nil)
	proxyURL, err := connect.Transport.(*http.Transport).Proxy(request)
	if err != nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("CONNECT proxy = (%v, %v)", proxyURL, err)
	}

	if _, err := NewHTTPClient(ProtocolSOCKS5, 2080); err != nil {
		t.Fatalf("SOCKS5 client error = %v", err)
	}
	for _, invalid := range []Protocol{"ftp", "https", "socks5h"} {
		if _, err := NewHTTPClient(invalid, 7890); err == nil {
			t.Errorf("protocol %q accepted", invalid)
		}
	}
}
