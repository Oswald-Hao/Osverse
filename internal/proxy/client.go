package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"

	xproxy "golang.org/x/net/proxy"
)

const directProtocol Protocol = ""

// NewHTTPClient returns a hardened direct or loopback-only client. The caller
// still owns redirect and response-size policy for its fixed artifact URL.
func NewHTTPClient(protocol Protocol, port int) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.IdleConnTimeout = 30 * time.Second
	if protocol == directProtocol {
		return &http.Client{Transport: transport, Timeout: 10 * time.Minute}, nil
	}
	proxyAddress, err := proxyURL(protocol, port)
	if err != nil {
		return nil, err
	}
	switch protocol {
	case ProtocolHTTP, ProtocolHTTPSConnect:
		parsed, err := http.NewRequest(http.MethodGet, proxyAddress, nil)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed.URL)
	case ProtocolSOCKS5:
		dialer, err := xproxy.SOCKS5("tcp", parsedHost(proxyAddress), nil, &net.Dialer{Timeout: 15 * time.Second})
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(xproxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS5 dialer lacks context support")
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return contextDialer.DialContext(ctx, network, address)
		}
	default:
		return nil, errors.New("unsupported proxy protocol")
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Minute}, nil
}

func parsedHost(proxyURL string) string {
	const prefix = "socks5://"
	if len(proxyURL) > len(prefix) && proxyURL[:len(prefix)] == prefix {
		return proxyURL[len(prefix):]
	}
	return ""
}
