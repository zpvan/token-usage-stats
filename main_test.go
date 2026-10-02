package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHandleMethodUnknown(t *testing.T) {
	raw, err := handleMethod("no.such.method", nil)
	if err != nil {
		t.Fatalf("handleMethod returned error: %v", err)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		t.Fatalf("response is not a valid envelope: %v", errUnmarshal)
	}
	if env.OK {
		t.Fatal("expected ok=false for unknown method")
	}
	if env.Error == nil || env.Error.Code != "unknown_method" {
		t.Fatalf("expected unknown_method error, got %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "no.such.method") {
		t.Fatalf("error message should mention the method, got %q", env.Error.Message)
	}
}

func TestOkEnvelopeRoundTrip(t *testing.T) {
	raw, err := okEnvelope(struct{ Ping string }{Ping: "pong"})
	if err != nil {
		t.Fatalf("okEnvelope: %v", err)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		t.Fatalf("unmarshal: %v", errUnmarshal)
	}
	if !env.OK || env.Error != nil {
		t.Fatalf("expected ok envelope, got %+v", env)
	}
	var result struct{ Ping string }
	if errUnmarshal := json.Unmarshal(env.Result, &result); errUnmarshal != nil {
		t.Fatalf("unmarshal result: %v", errUnmarshal)
	}
	if result.Ping != "pong" {
		t.Fatalf("Ping = %q, want pong", result.Ping)
	}
}
