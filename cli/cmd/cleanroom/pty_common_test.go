package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLockedBufferBoundariesAndSearch(t *testing.T) {
	buffer := &lockedBuffer{}
	payload := strings.Repeat("x", maximumPTYTranscriptBytes-4) + "done"
	if written, err := buffer.Write([]byte(payload)); err != nil || written != len(payload) {
		t.Fatalf("exact fill = %d, %v", written, err)
	}
	if !buffer.ContainsAfter(-1, "done") || buffer.ContainsAfter(buffer.Len(), "done") {
		t.Fatal("ContainsAfter boundary mismatch")
	}
	if written, err := buffer.Write([]byte("!")); err == nil || written != 0 || !buffer.Overflowed() {
		t.Fatalf("overflow = %d, %v, overflowed=%v", written, err, buffer.Overflowed())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := awaitCandidatePTYText(ctx, buffer, "missing", time.Second); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("overflow wait error = %v", err)
	}
}

func TestUnsupportedProtocolResponseRequiresSingleExactJSONValue(t *testing.T) {
	valid := `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"unsupported protocolVersion","data":{"supported":"2025-03-26"}}}`
	if !isUnsupportedProtocolResponse([]byte(valid), "2025-03-26") {
		t.Fatal("valid unsupported-protocol response was rejected")
	}
	for _, invalid := range []string{valid + ` {"jsonrpc":"2.0","id":2,"result":{}}`, valid + " trailing", valid + "\n{}"} {
		if isUnsupportedProtocolResponse([]byte(invalid), "2025-03-26") {
			t.Fatalf("trailing response was accepted: %q", invalid)
		}
	}
}
