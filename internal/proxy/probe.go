// Package proxy probes a user-selected loopback proxy port without accepting
// arbitrary hosts or network targets from the frontend.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"
)

// Protocol is one supported local proxy handshake.
type Protocol string

const (
	ProtocolHTTP         Protocol = "http"
	ProtocolHTTPSConnect Protocol = "https-connect"
	ProtocolSOCKS5       Protocol = "socks5"
)

const availableMessage = "可用"

var (
	ErrInvalidPort            = errors.New("invalid proxy port")
	errHandshake              = errors.New("proxy handshake failed")
	errAuthenticationRequired = errors.New("proxy authentication required")
	errTLSVerification        = errors.New("proxy TLS verification failed")
	errTargetUnavailable      = errors.New("proxy target unavailable")
)

// Attempt is the frontend-safe outcome of one fixed protocol probe.
type Attempt struct {
	Protocol      Protocol `json:"protocol"`
	Available     bool     `json:"available"`
	LatencyMillis int64    `json:"latencyMillis"`
	Message       string   `json:"message"`
}

// Result contains all protocol outcomes for one loopback port. Recommended is
// empty unless the port can carry a certificate-verified HTTPS download.
type Result struct {
	Port        int       `json:"port"`
	Reachable   bool      `json:"reachable"`
	Recommended Protocol  `json:"recommended"`
	Attempts    []Attempt `json:"attempts"`
	CheckedAt   time.Time `json:"checkedAt"`
}

type probeTarget struct {
	host       string
	port       string
	path       string
	serverName string
}

var productionTarget = probeTarget{
	host:       "registry.npmjs.org",
	port:       "443",
	path:       "/-/ping",
	serverName: "registry.npmjs.org",
}

type protocolProbe interface {
	Protocol() Protocol
	Check(context.Context, string, probeTarget) (time.Duration, error)
}

// Service performs the three fixed proxy probes.
type Service struct {
	probes []protocolProbe
	target probeTarget
	clock  func() time.Time
}

// NewService constructs the production loopback proxy prober.
func NewService() *Service {
	return &Service{
		probes: []protocolProbe{
			newHTTPProbe(),
			newHTTPSConnectProbe(),
			newSOCKS5Probe(),
		},
		target: productionTarget,
		clock:  time.Now,
	}
}

// Probe validates one port and returns stable, redacted protocol outcomes.
func (service *Service) Probe(ctx context.Context, port int) (Result, error) {
	if port < 1 || port > 65535 {
		return Result{}, ErrInvalidPort
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if service == nil || service.clock == nil {
		return Result{}, errors.New("proxy service unavailable")
	}

	address := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	probeByProtocol := make(map[Protocol]protocolProbe, len(service.probes))
	for _, probe := range service.probes {
		if probe != nil {
			probeByProtocol[probe.Protocol()] = probe
		}
	}

	result := Result{
		Port:      port,
		Attempts:  make([]Attempt, 0, 3),
		CheckedAt: service.clock(),
	}
	type probeOutcome struct {
		attempt Attempt
	}
	outcomes := make(chan probeOutcome, 3)
	protocols := []Protocol{ProtocolHTTP, ProtocolHTTPSConnect, ProtocolSOCKS5}
	for _, protocol := range protocols {
		probe := probeByProtocol[protocol]
		go func() {
			attempt := Attempt{Protocol: protocol, Message: unavailableMessage(errHandshake)}
			if probe != nil {
				latency, err := probe.Check(ctx, address, service.target)
				if err == nil {
					attempt.Available = true
					attempt.LatencyMillis = latency.Milliseconds()
					if attempt.LatencyMillis < 1 {
						attempt.LatencyMillis = 1
					}
					attempt.Message = availableMessage
				} else {
					attempt.Message = unavailableMessage(err)
				}
			}
			outcomes <- probeOutcome{attempt: attempt}
		}()
	}

	available := make(map[Protocol]bool, 3)
	for range protocols {
		select {
		case outcome := <-outcomes:
			result.Attempts = append(result.Attempts, outcome.attempt)
			available[outcome.attempt.Protocol] = outcome.attempt.Available
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	stableAttempts(result.Attempts)

	// CONNECT is preferred because Go's HTTP transport can use it natively.
	// SOCKS5 is the fallback. Plain HTTP without CONNECT cannot securely carry
	// the HTTPS-only artifact downloads and is therefore informational only.
	switch {
	case available[ProtocolHTTPSConnect]:
		result.Recommended = ProtocolHTTPSConnect
	case available[ProtocolSOCKS5]:
		result.Recommended = ProtocolSOCKS5
	}
	result.Reachable = result.Recommended != ""
	return result, nil
}

func unavailableMessage(err error) string {
	switch {
	case errors.Is(err, errAuthenticationRequired):
		return "代理需要身份验证"
	case errors.Is(err, context.DeadlineExceeded):
		return "连接超时"
	case errors.Is(err, context.Canceled):
		return "探测已取消"
	case errors.Is(err, errTLSVerification):
		return "TLS 证书验证失败"
	case errors.Is(err, errTargetUnavailable):
		return "代理无法访问验证目标"
	default:
		return "协议握手失败"
	}
}

// stableAttempts is used by callers that persist a result assembled from
// concurrent sources in future versions.
func stableAttempts(attempts []Attempt) {
	order := map[Protocol]int{ProtocolHTTP: 0, ProtocolHTTPSConnect: 1, ProtocolSOCKS5: 2}
	sort.SliceStable(attempts, func(i, j int) bool { return order[attempts[i].Protocol] < order[attempts[j].Protocol] })
}
