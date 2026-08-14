package profiles

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	probeBodyLimit = 64 * 1024
	probeTimeout   = 12 * time.Second
)

var (
	ErrPrivateEndpoint = errors.New("private API endpoint requires confirmation")
	ErrProbeFailed     = errors.New("API profile probe failed")
)

// ProtocolResult is one no-inference compatibility observation.
type ProtocolResult struct {
	Protocol string `json:"protocol"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// ProbeResult never contains credentials or response bodies.
type ProbeResult struct {
	ProfileID     string           `json:"profileId"`
	Reachable     bool             `json:"reachable"`
	Authenticated bool             `json:"authenticated"`
	Protocols     []ProtocolResult `json:"protocols"`
	Message       string           `json:"message"`
	CheckedAt     time.Time        `json:"checkedAt"`
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Prober performs fixed, no-inference compatibility requests.
type Prober struct {
	resolver      ipResolver
	now           func() time.Time
	clientFactory func(string, []net.IP) (httpDoer, error)
}

func NewProber() *Prober {
	return &Prober{
		resolver: net.DefaultResolver,
		now:      time.Now,
		clientFactory: func(serverName string, addresses []net.IP) (httpDoer, error) {
			return pinnedHTTPClient(serverName, addresses)
		},
	}
}

// Probe validates DNS policy, then checks models and fixed protocol routes.
func (prober *Prober) Probe(ctx context.Context, input Input) (ProbeResult, error) {
	if prober == nil || prober.resolver == nil || prober.now == nil || prober.clientFactory == nil {
		return ProbeResult{}, ErrProbeFailed
	}
	validated, normalized, err := validateInput(input)
	if err != nil {
		return ProbeResult{}, err
	}
	validated.BaseURL = normalized
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	parsed, _ := url.Parse(validated.BaseURL)
	addresses, err := resolveEndpoint(ctx, prober.resolver, parsed.Hostname(), validated.AllowPrivateNetwork)
	if err != nil {
		return ProbeResult{}, err
	}
	client, err := prober.clientFactory(parsed.Hostname(), addresses)
	if err != nil || client == nil {
		return ProbeResult{}, ErrProbeFailed
	}
	result := ProbeResult{
		ProfileID: validated.ID, CheckedAt: prober.now().UTC(),
		Protocols: []ProtocolResult{
			{Protocol: "openai-responses", Status: "unconfirmed", Message: "尚未确认 Responses 路由"},
			{Protocol: "openai-chat", Status: "unconfirmed", Message: "尚未确认 Chat Completions 路由"},
			{Protocol: "anthropic-messages", Status: "unconfirmed", Message: "尚未确认 Anthropic Messages 路由"},
		},
	}
	base := strings.TrimRight(validated.BaseURL, "/")
	modelStatus, modelErr := prober.request(ctx, client, http.MethodGet, endpointURL(base, "models"), validated.APIKey)
	if modelErr == nil {
		result.Reachable = true
		result.Authenticated = modelStatus >= 200 && modelStatus < 300
	}
	routes := []struct {
		index int
		path  string
	}{
		{index: 0, path: "responses"},
		{index: 1, path: "chat/completions"},
		{index: 2, path: "messages"},
	}
	for _, route := range routes {
		status, requestErr := prober.request(ctx, client, http.MethodOptions, endpointURL(base, route.path), validated.APIKey)
		if requestErr != nil {
			continue
		}
		result.Reachable = true
		result.Protocols[route.index] = classifyProtocol(result.Protocols[route.index].Protocol, status)
	}
	if !result.Reachable {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ProbeResult{}, ctxErr
		}
		return ProbeResult{}, ErrProbeFailed
	}
	if result.Authenticated {
		result.Message = "API 可访问，凭据已通过模型列表验证"
	} else {
		result.Message = "API 可访问，但凭据或模型列表尚未验证"
	}
	return result, nil
}

func resolveEndpoint(ctx context.Context, resolver ipResolver, host string, allowPrivate bool) ([]net.IP, error) {
	var addresses []net.IP
	if literal := net.ParseIP(host); literal != nil {
		addresses = []net.IP{literal}
	} else {
		resolved, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(resolved) == 0 {
			return nil, ErrProbeFailed
		}
		for _, address := range resolved {
			if address.IP != nil {
				addresses = append(addresses, append(net.IP(nil), address.IP...))
			}
		}
	}
	if len(addresses) == 0 {
		return nil, ErrProbeFailed
	}
	for _, address := range addresses {
		if unsafeIPAddress(address) && !allowPrivate {
			return nil, ErrPrivateEndpoint
		}
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].String() < addresses[j].String() })
	return addresses, nil
}

func unsafeIPAddress(address net.IP) bool {
	return address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() || reservedIPAddress(address) ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast()
}

func reservedIPAddress(address net.IP) bool {
	reservedCIDRs := []string{
		"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32",
	}
	for _, raw := range reservedCIDRs {
		_, network, _ := net.ParseCIDR(raw)
		if network.Contains(address) {
			return true
		}
	}
	return false
}

func pinnedHTTPClient(serverName string, addresses []net.IP) (*http.Client, error) {
	if serverName == "" || len(addresses) == 0 {
		return nil, ErrProbeFailed
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 20 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrProbeFailed
		}
		var lastErr error
		for _, ip := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport: transport, Timeout: probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("API redirects are disabled") },
	}, nil
}

func endpointURL(base, endpoint string) string {
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + endpoint
	}
	return base + "/v1/" + endpoint
}

func (prober *Prober) request(ctx context.Context, client httpDoer, method, endpoint, key string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return 0, ErrProbeFailed
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("X-Api-Key", key)
	request.Header.Set("Anthropic-Version", "2023-06-01")
	request.Header.Set("User-Agent", "Osverse-API-Probe")
	response, err := client.Do(request)
	if err != nil {
		return 0, ErrProbeFailed
	}
	defer response.Body.Close()
	if method == http.MethodGet && response.StatusCode >= 200 && response.StatusCode < 300 {
		limited, err := io.ReadAll(io.LimitReader(response.Body, probeBodyLimit+1))
		if err != nil || len(limited) > probeBodyLimit || !json.Valid(limited) {
			return 0, ErrProbeFailed
		}
	}
	return response.StatusCode, nil
}

func classifyProtocol(protocol string, status int) ProtocolResult {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return ProtocolResult{Protocol: protocol, Status: "unavailable", Message: "服务未提供该路由"}
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent,
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusMethodNotAllowed, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return ProtocolResult{Protocol: protocol, Status: "compatible", Message: "已识别协议路由"}
	default:
		return ProtocolResult{Protocol: protocol, Status: "unconfirmed", Message: fmt.Sprintf("路由返回 HTTP %d", status)}
	}
}
