package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNewPublicErrorRedactsCause(t *testing.T) {
	cause := errors.New("token=sk-secret")
	public := NewPublicError(ErrCommandFailed, "codex version failed", cause)

	if got := public.Error(); !strings.Contains(got, "COMMAND_FAILED") || !strings.Contains(got, "codex version failed") {
		t.Errorf("Error() = %q, want code and public message", got)
	}
	if got := public.Error(); strings.Contains(got, "sk-secret") {
		t.Errorf("Error() exposed cause text: %q", got)
	}
	if !errors.Is(public, cause) {
		t.Error("Unwrap() did not retain the backend cause")
	}

	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("json.Marshal(PublicError) returned error: %v", err)
	}
	if got, want := string(encoded), `{"code":"COMMAND_FAILED","message":"codex version failed"}`; got != want {
		t.Errorf("json.Marshal(PublicError) = %s, want %s", got, want)
	}
	if strings.Contains(string(encoded), "sk-secret") {
		t.Errorf("JSON exposed cause text: %s", encoded)
	}
}
