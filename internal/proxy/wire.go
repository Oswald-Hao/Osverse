package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	probeTimeout     = 5 * time.Second
	probeHeaderLimit = 64 * 1024
)

type wireProbe struct {
	protocol  Protocol
	dialer    *net.Dialer
	tlsConfig func(probeTarget) *tls.Config
}

func newHTTPProbe() protocolProbe {
	return productionWireProbe(ProtocolHTTP)
}

func newHTTPSConnectProbe() protocolProbe {
	return productionWireProbe(ProtocolHTTPSConnect)
}

func newSOCKS5Probe() protocolProbe {
	return productionWireProbe(ProtocolSOCKS5)
}

func productionWireProbe(protocol Protocol) wireProbe {
	return wireProbe{
		protocol: protocol,
		dialer:   &net.Dialer{Timeout: probeTimeout},
		tlsConfig: func(target probeTarget) *tls.Config {
			return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.serverName}
		},
	}
}

func (probe wireProbe) Protocol() Protocol { return probe.protocol }

func (probe wireProbe) Check(ctx context.Context, address string, target probeTarget) (time.Duration, error) {
	if probe.dialer == nil {
		return 0, errHandshake
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	started := time.Now()
	conn, err := probe.dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, classifyWireError(ctx, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return 0, errHandshake
		}
	}

	switch probe.protocol {
	case ProtocolHTTP:
		err = checkHTTPProxy(conn, target)
	case ProtocolHTTPSConnect:
		err = checkHTTPSConnect(conn, target, probe.tlsConfig)
	case ProtocolSOCKS5:
		err = checkSOCKS5(conn, target, probe.tlsConfig)
	default:
		err = errHandshake
	}
	if err != nil {
		return 0, classifyWireError(ctx, err)
	}
	return time.Since(started), nil
}

func checkHTTPProxy(conn net.Conn, target probeTarget) error {
	request := "GET http://" + net.JoinHostPort(target.host, "80") + target.path + " HTTP/1.1\r\n" +
		"Host: " + target.host + "\r\nConnection: close\r\nUser-Agent: Osverse-Proxy-Probe\r\n\r\n"
	if err := writeFull(conn, []byte(request)); err != nil {
		return err
	}
	response, err := readBoundedResponse(conn, http.MethodGet)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return classifyHTTPStatus(response.StatusCode)
}

func checkHTTPSConnect(conn net.Conn, target probeTarget, tlsConfig func(probeTarget) *tls.Config) error {
	authority := net.JoinHostPort(target.host, target.port)
	request := "CONNECT " + authority + " HTTP/1.1\r\nHost: " + authority +
		"\r\nProxy-Connection: keep-alive\r\nUser-Agent: Osverse-Proxy-Probe\r\n\r\n"
	if err := writeFull(conn, []byte(request)); err != nil {
		return err
	}
	response, err := readConnectResponse(conn)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusProxyAuthRequired {
		return errAuthenticationRequired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errTargetUnavailable
	}

	return verifyHTTPS(conn, target, tlsConfig)
}

func checkSOCKS5(conn net.Conn, target probeTarget, tlsConfig func(probeTarget) *tls.Config) error {
	if err := writeFull(conn, []byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(conn, method); err != nil {
		return err
	}
	if method[0] != 0x05 {
		return errHandshake
	}
	if method[1] == 0xff || method[1] != 0x00 {
		return errAuthenticationRequired
	}
	if len(target.host) == 0 || len(target.host) > 255 {
		return errHandshake
	}
	port, err := strconv.ParseUint(target.port, 10, 16)
	if err != nil || port == 0 {
		return errHandshake
	}
	request := make([]byte, 0, len(target.host)+7)
	request = append(request, 0x05, 0x01, 0x00, 0x03, byte(len(target.host)))
	request = append(request, target.host...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	request = append(request, portBytes...)
	if err := writeFull(conn, request); err != nil {
		return err
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 || header[1] != 0x00 {
		return errTargetUnavailable
	}
	addressLength := 0
	switch header[3] {
	case 0x01:
		addressLength = net.IPv4len
	case 0x04:
		addressLength = net.IPv6len
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		addressLength = int(length[0])
	default:
		return errHandshake
	}
	if addressLength == 0 {
		return errHandshake
	}
	boundAddressAndPort := make([]byte, addressLength+2)
	if _, err := io.ReadFull(conn, boundAddressAndPort); err != nil {
		return err
	}
	return verifyHTTPS(conn, target, tlsConfig)
}

func verifyHTTPS(conn net.Conn, target probeTarget, tlsConfig func(probeTarget) *tls.Config) error {
	if tlsConfig == nil {
		return errTLSVerification
	}
	config := tlsConfig(target)
	if config == nil || config.InsecureSkipVerify || config.ServerName == "" {
		return errTLSVerification
	}
	tlsConn := tls.Client(conn, config)
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("%w: %v", errTLSVerification, err)
	}
	request := "HEAD " + target.path + " HTTP/1.1\r\nHost: " + target.host +
		"\r\nConnection: close\r\nUser-Agent: Osverse-Proxy-Probe\r\n\r\n"
	if err := writeFull(tlsConn, []byte(request)); err != nil {
		return err
	}
	response, err := readBoundedResponse(tlsConn, http.MethodHead)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return classifyHTTPStatus(response.StatusCode)
}

func readConnectResponse(conn net.Conn) (*http.Response, error) {
	header := make([]byte, 0, 512)
	one := []byte{0}
	for len(header) < probeHeaderLimit {
		if _, err := io.ReadFull(conn, one); err != nil {
			return nil, err
		}
		header = append(header, one[0])
		if len(header) >= 4 && bytes.Equal(header[len(header)-4:], []byte("\r\n\r\n")) {
			return http.ReadResponse(
				bufio.NewReader(bytes.NewReader(header)),
				&http.Request{Method: http.MethodConnect},
			)
		}
	}
	return nil, errors.New("proxy response headers exceed limit")
}

func readBoundedResponse(reader io.Reader, method string) (*http.Response, error) {
	return http.ReadResponse(
		bufio.NewReader(io.LimitReader(reader, probeHeaderLimit)),
		&http.Request{Method: method},
	)
}

func classifyHTTPStatus(status int) error {
	switch {
	case status == http.StatusProxyAuthRequired:
		return errAuthenticationRequired
	case status >= 200 && status < 500:
		return nil
	default:
		return errTargetUnavailable
	}
}

func classifyWireError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return context.DeadlineExceeded
	}
	return err
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func proxyURL(protocol Protocol, port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", ErrInvalidPort
	}
	host := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	switch protocol {
	case ProtocolHTTPSConnect, ProtocolHTTP:
		return "http://" + host, nil
	case ProtocolSOCKS5:
		return "socks5://" + host, nil
	default:
		return "", errors.New("unsupported proxy protocol")
	}
}

func redactedNetworkError(err error) string {
	if err == nil {
		return ""
	}
	message := unavailableMessage(err)
	return strings.TrimSpace(message)
}
