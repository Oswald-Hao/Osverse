package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServiceRejectsInvalidPortsWithoutProbing(t *testing.T) {
	for _, port := range []int{-1, 0, 65536} {
		called := false
		service := newServiceForTest([]protocolProbe{fakeProtocolProbe{
			protocol: ProtocolHTTP,
			check: func(context.Context, string, probeTarget) error {
				called = true
				return nil
			},
		}})

		result, err := service.Probe(context.Background(), port)

		if !errors.Is(err, ErrInvalidPort) {
			t.Fatalf("port %d error = %v, want ErrInvalidPort", port, err)
		}
		if called {
			t.Fatalf("port %d invoked a protocol probe", port)
		}
		if !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("port %d result = %#v, want zero", port, result)
		}
	}
}

func TestServiceUsesOnlyLoopbackAndReturnsStableProtocolOrder(t *testing.T) {
	wantAddress := "127.0.0.1:7890"
	wantTime := time.Date(2026, time.August, 14, 2, 3, 4, 0, time.UTC)
	probes := []protocolProbe{
		fakeProtocolProbe{protocol: ProtocolSOCKS5, latency: 7 * time.Millisecond},
		fakeProtocolProbe{protocol: ProtocolHTTP, latency: 3 * time.Millisecond},
		fakeProtocolProbe{protocol: ProtocolHTTPSConnect, latency: 5 * time.Millisecond},
	}
	for index := range probes {
		probe := probes[index].(fakeProtocolProbe)
		probe.check = func(_ context.Context, address string, target probeTarget) error {
			if address != wantAddress {
				t.Errorf("address = %q, want loopback %q", address, wantAddress)
			}
			if target.host != productionTarget.host || target.path != productionTarget.path {
				t.Errorf("target = %#v, want fixed production target", target)
			}
			return nil
		}
		probes[index] = probe
	}
	service := newServiceForTest(probes)
	service.clock = func() time.Time { return wantTime }

	result, err := service.Probe(context.Background(), 7890)

	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if result.Port != 7890 || !result.Reachable || result.CheckedAt != wantTime {
		t.Fatalf("result metadata = %#v", result)
	}
	if result.Recommended != ProtocolHTTPSConnect {
		t.Fatalf("Recommended = %q, want %q", result.Recommended, ProtocolHTTPSConnect)
	}
	wantProtocols := []Protocol{ProtocolHTTP, ProtocolHTTPSConnect, ProtocolSOCKS5}
	gotProtocols := make([]Protocol, 0, len(result.Attempts))
	for _, attempt := range result.Attempts {
		gotProtocols = append(gotProtocols, attempt.Protocol)
		if !attempt.Available || attempt.LatencyMillis <= 0 || attempt.Message != availableMessage {
			t.Errorf("successful attempt = %#v", attempt)
		}
	}
	if !reflect.DeepEqual(gotProtocols, wantProtocols) {
		t.Fatalf("protocol order = %#v, want %#v", gotProtocols, wantProtocols)
	}
}

func TestServiceRecommendationRequiresHTTPSCapableProtocol(t *testing.T) {
	tests := []struct {
		name string
		fail map[Protocol]error
		want Protocol
	}{
		{name: "connect preferred", fail: map[Protocol]error{}, want: ProtocolHTTPSConnect},
		{name: "socks fallback", fail: map[Protocol]error{ProtocolHTTPSConnect: errHandshake}, want: ProtocolSOCKS5},
		{name: "plain HTTP is not download capable", fail: map[Protocol]error{
			ProtocolHTTPSConnect: errHandshake, ProtocolSOCKS5: errHandshake,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newServiceForTest([]protocolProbe{
				fakeProtocolProbe{protocol: ProtocolHTTP, err: tt.fail[ProtocolHTTP]},
				fakeProtocolProbe{protocol: ProtocolHTTPSConnect, err: tt.fail[ProtocolHTTPSConnect]},
				fakeProtocolProbe{protocol: ProtocolSOCKS5, err: tt.fail[ProtocolSOCKS5]},
			})

			result, err := service.Probe(context.Background(), 8080)

			if err != nil {
				t.Fatalf("Probe() error = %v", err)
			}
			if result.Recommended != tt.want {
				t.Fatalf("Recommended = %q, want %q", result.Recommended, tt.want)
			}
			if result.Reachable != (tt.want != "") {
				t.Fatalf("Reachable = %t, want %t", result.Reachable, tt.want != "")
			}
		})
	}
}

func TestServiceCancellationAndFailuresAreRedacted(t *testing.T) {
	secret := "proxy-user:proxy-password"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := newServiceForTest([]protocolProbe{
		fakeProtocolProbe{protocol: ProtocolHTTP, err: errors.New(secret)},
		fakeProtocolProbe{protocol: ProtocolHTTPSConnect, err: context.Canceled},
		fakeProtocolProbe{protocol: ProtocolSOCKS5, err: errAuthenticationRequired},
	})

	result, err := service.Probe(ctx, 3128)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe() error = %v, want context cancellation", err)
	}
	for _, attempt := range result.Attempts {
		if strings.Contains(attempt.Message, secret) {
			t.Fatalf("attempt leaked private error: %#v", attempt)
		}
	}
}

func TestWireHTTPProbeRequiresAValidProxyResponse(t *testing.T) {
	listener := listenLoopback(t)
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			done <- err
			return
		}
		if request.URL.Scheme != "http" || request.URL.Hostname() != productionTarget.host || request.URL.Path != productionTarget.path {
			done <- fmt.Errorf("unexpected absolute proxy request: %s", request.URL)
			return
		}
		_, err = io.WriteString(conn, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		done <- err
	}()

	latency, err := productionWireProbe(ProtocolHTTP).Check(context.Background(), listener.Addr().String(), productionTarget)

	if err != nil || latency <= 0 {
		t.Fatalf("HTTP probe = (%v, %v), want success", latency, err)
	}
	if serverErr := <-done; serverErr != nil {
		t.Fatalf("fake HTTP proxy: %v", serverErr)
	}
}

func TestSuccessfulWireLatencyIsPositiveEvenBeforeTheClockTicks(t *testing.T) {
	if got := positiveWireLatency(0); got != time.Nanosecond {
		t.Fatalf("zero elapsed latency = %v", got)
	}
	if got := positiveWireLatency(-time.Nanosecond); got != time.Nanosecond {
		t.Fatalf("negative elapsed latency = %v", got)
	}
	if got := positiveWireLatency(7 * time.Millisecond); got != 7*time.Millisecond {
		t.Fatalf("positive elapsed latency = %v", got)
	}
}

func TestWireHTTPSConnectAndSOCKS5VerifyTheTargetCertificate(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead || request.URL.Path != "/probe" {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer tlsServer.Close()
	target := targetForTLSServer(t, tlsServer)
	tlsConfig := trustedTestTLSConfig(tlsServer)

	for _, tt := range []struct {
		protocol Protocol
		serve    func(*testing.T, net.Listener, string)
	}{
		{protocol: ProtocolHTTPSConnect, serve: serveConnectProxy},
		{protocol: ProtocolSOCKS5, serve: serveSOCKS5Proxy},
	} {
		t.Run(string(tt.protocol), func(t *testing.T) {
			listener := listenLoopback(t)
			done := make(chan struct{})
			go func() {
				defer close(done)
				tt.serve(t, listener, net.JoinHostPort(target.host, target.port))
			}()
			probe := productionWireProbe(tt.protocol)
			probe.tlsConfig = tlsConfig

			latency, err := probe.Check(context.Background(), listener.Addr().String(), target)

			if err != nil || latency <= 0 {
				t.Fatalf("%s probe = (%v, %v), want certificate-verified success", tt.protocol, latency, err)
			}
			<-done
		})
	}
}

func TestWireHTTPSConnectRejectsAuthenticationAndUntrustedTLS(t *testing.T) {
	t.Run("authentication", func(t *testing.T) {
		listener := listenLoopback(t)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = http.ReadRequest(bufio.NewReader(conn))
			_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
		}()

		_, err := productionWireProbe(ProtocolHTTPSConnect).Check(context.Background(), listener.Addr().String(), productionTarget)

		if !errors.Is(err, errAuthenticationRequired) {
			t.Fatalf("error = %v, want authentication-required classification", err)
		}
	})

	t.Run("untrusted target certificate", func(t *testing.T) {
		tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer tlsServer.Close()
		target := targetForTLSServer(t, tlsServer)
		listener := listenLoopback(t)
		go serveConnectProxy(t, listener, net.JoinHostPort(target.host, target.port))

		_, err := productionWireProbe(ProtocolHTTPSConnect).Check(context.Background(), listener.Addr().String(), target)

		if !errors.Is(err, errTLSVerification) {
			t.Fatalf("error = %v, want TLS verification failure", err)
		}
	})
}

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func targetForTLSServer(t *testing.T, server *httptest.Server) probeTarget {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse TLS server URL: %v", err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split TLS server address: %v", err)
	}
	return probeTarget{host: host, port: port, path: "/probe", serverName: "example.com"}
}

func trustedTestTLSConfig(server *httptest.Server) func(probeTarget) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	return func(target probeTarget) *tls.Config {
		return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.serverName, RootCAs: pool}
	}
}

func serveConnectProxy(t *testing.T, listener net.Listener, targetAddress string) {
	t.Helper()
	client, err := listener.Accept()
	if err != nil {
		return
	}
	defer client.Close()
	request, err := http.ReadRequest(bufio.NewReader(client))
	if err != nil || request.Method != http.MethodConnect || request.Host != targetAddress {
		return
	}
	target, err := net.Dial("tcp", targetAddress)
	if err != nil {
		return
	}
	defer target.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	proxyBidirectionally(client, target)
}

func serveSOCKS5Proxy(t *testing.T, listener net.Listener, targetAddress string) {
	t.Helper()
	client, err := listener.Accept()
	if err != nil {
		return
	}
	defer client.Close()
	methods := make([]byte, 3)
	if _, err := io.ReadFull(client, methods); err != nil || !reflect.DeepEqual(methods, []byte{0x05, 0x01, 0x00}) {
		return
	}
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}
	header := make([]byte, 5)
	if _, err := io.ReadFull(client, header); err != nil || !reflect.DeepEqual(header[:4], []byte{0x05, 0x01, 0x00, 0x03}) {
		return
	}
	hostLength := int(header[4])
	hostAndPort := make([]byte, hostLength+2)
	if _, err := io.ReadFull(client, hostAndPort); err != nil {
		return
	}
	host := string(hostAndPort[:hostLength])
	port := binary.BigEndian.Uint16(hostAndPort[hostLength:])
	if net.JoinHostPort(host, strconv.Itoa(int(port))) != targetAddress {
		return
	}
	target, err := net.Dial("tcp", targetAddress)
	if err != nil {
		return
	}
	defer target.Close()
	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	proxyBidirectionally(client, target)
}

func proxyBidirectionally(left, right net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(left, right); done <- struct{}{} }()
	go func() { _, _ = io.Copy(right, left); done <- struct{}{} }()
	<-done
}

type fakeProtocolProbe struct {
	protocol Protocol
	latency  time.Duration
	err      error
	check    func(context.Context, string, probeTarget) error
}

func (probe fakeProtocolProbe) Protocol() Protocol { return probe.protocol }

func (probe fakeProtocolProbe) Check(ctx context.Context, address string, target probeTarget) (time.Duration, error) {
	if probe.check != nil {
		if err := probe.check(ctx, address, target); err != nil {
			return 0, err
		}
	}
	if probe.err != nil {
		return 0, probe.err
	}
	if probe.latency <= 0 {
		return time.Millisecond, nil
	}
	return probe.latency, nil
}

func newServiceForTest(probes []protocolProbe) *Service {
	return &Service{
		probes: probes,
		target: productionTarget,
		clock:  time.Now,
	}
}
