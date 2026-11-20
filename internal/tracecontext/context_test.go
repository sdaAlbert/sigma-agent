package tracecontext

import "testing"

func TestParseTraceparentPreservesFlags(t *testing.T) {
	parsed, err := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-03")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TraceFlags != "03" {
		t.Fatalf("TraceFlags = %q, want %q", parsed.TraceFlags, "03")
	}
	if got := parsed.Traceparent(); got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-03" {
		t.Fatalf("Traceparent() = %q", got)
	}
}
