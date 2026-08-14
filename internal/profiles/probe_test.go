package profiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolveEndpointRejectsAnyPrivateAnswerWithoutConfirmation(t *testing.T) {
	resolver := fakeResolver{addresses: []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("127.0.0.1")},
	}}
	if _, err := resolveEndpoint(context.Background(), resolver, "api.example", false); !errors.Is(err, ErrPrivateEndpoint) {
		t.Fatalf("mixed DNS answer error = %v", err)
	}
	addresses, err := resolveEndpoint(context.Background(), resolver, "api.example", true)
	if err != nil || len(addresses) != 2 {
		t.Fatalf("confirmed private addresses = (%v, %v)", addresses, err)
	}
	if _, err := resolveEndpoint(context.Background(), resolver, "169.254.169.254", false); !errors.Is(err, ErrPrivateEndpoint) {
		t.Fatalf("metadata address error = %v", err)
	}
}

func TestProbeUsesFixedNoInferenceRoutesAndRedactsCredentials(t *testing.T) {
	secret := "key-super-secret"
	var requests []*http.Request
	prober := &Prober{
		resolver: fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}},
		now:      func() time.Time { return time.Date(2026, time.August, 14, 1, 2, 3, 0, time.UTC) },
		clientFactory: func(serverName string, addresses []net.IP) (httpDoer, error) {
			if serverName != "api.example" || len(addresses) != 1 {
				t.Fatalf("client target = %q %#v", serverName, addresses)
			}
			return doerFunc(func(request *http.Request) (*http.Response, error) {
				requests = append(requests, request.Clone(request.Context()))
				status := http.StatusNoContent
				body := []byte{}
				switch request.URL.Path {
				case "/gateway/v1/models":
					status = http.StatusOK
					body = []byte(`{"data":[]}`)
				case "/gateway/v1/responses":
					status = http.StatusMethodNotAllowed
				case "/gateway/v1/chat/completions":
					status = http.StatusNotFound
				case "/gateway/v1/messages":
					status = http.StatusUnauthorized
				default:
					t.Fatalf("unexpected probe path %q", request.URL.Path)
				}
				return response(request, status, body), nil
			}), nil
		},
	}

	result, err := prober.Probe(context.Background(), Input{
		ID: "profile", Name: "Profile", APIKey: secret,
		BaseURL: "https://api.example/gateway/v1", Model: "model",
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Reachable || !result.Authenticated || result.Message == "" || len(result.Protocols) != 3 {
		t.Fatalf("result = %#v", result)
	}
	wantStatuses := []string{"compatible", "unavailable", "compatible"}
	gotStatuses := []string{result.Protocols[0].Status, result.Protocols[1].Status, result.Protocols[2].Status}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("statuses = %#v, want %#v", gotStatuses, wantStatuses)
	}
	for _, request := range requests {
		if request.Method != http.MethodGet && request.Method != http.MethodOptions {
			t.Errorf("probe method = %s", request.Method)
		}
		if request.Header.Get("Authorization") != "Bearer "+secret || request.Header.Get("X-Api-Key") != secret {
			t.Error("probe omitted fixed auth headers")
		}
	}
	formatted := strings.Join([]string{result.Message, result.Protocols[0].Message, result.Protocols[1].Message, result.Protocols[2].Message}, " ")
	if strings.Contains(formatted, secret) {
		t.Fatal("probe result leaked API key")
	}
}

func TestProbeRequiresBoundedValidModelsJSONForAuthentication(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "invalid JSON", body: []byte("not-json")},
		{name: "oversize", body: bytes.Repeat([]byte(" "), probeBodyLimit+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := testProber(doerFunc(func(request *http.Request) (*http.Response, error) {
				if strings.HasSuffix(request.URL.Path, "/models") {
					return response(request, http.StatusOK, test.body), nil
				}
				return response(request, http.StatusNotFound, nil), nil
			}))
			result, err := prober.Probe(context.Background(), testInput())
			if err != nil || !result.Reachable || result.Authenticated {
				t.Fatalf("Probe() = (%#v, %v)", result, err)
			}
		})
	}
}

func TestProbeReturnsOnlyStableErrorOnNetworkFailure(t *testing.T) {
	secret := "secret-network-key"
	prober := testProber(doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret)
	}))
	input := testInput()
	input.APIKey = secret
	result, err := prober.Probe(context.Background(), input)
	if !errors.Is(err, ErrProbeFailed) || strings.Contains(err.Error(), secret) || !reflect.DeepEqual(result, ProbeResult{}) {
		t.Fatalf("Probe() = (%#v, %v)", result, err)
	}
}

type fakeResolver struct {
	addresses []net.IPAddr
	err       error
}

func (resolver fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.addresses, resolver.err
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)), Request: request,
	}
}

func testProber(client httpDoer) *Prober {
	return &Prober{
		resolver:      fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}},
		now:           time.Now,
		clientFactory: func(string, []net.IP) (httpDoer, error) { return client, nil },
	}
}

func testInput() Input {
	return Input{
		ID: "profile", Name: "Profile", APIKey: "secret-key",
		BaseURL: "https://api.example/v1", Model: "model",
	}
}
