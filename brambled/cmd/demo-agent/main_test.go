package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunTaskCompletesPrivateHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":"report-private-service-health","host":"private-1","status":"healthy","observedAt":"2026-09-10T00:00:00Z"}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := runTask(context.Background(), server.Client(), server.URL, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "task complete") || !strings.Contains(out.String(), "private-1") {
		t.Fatalf("unexpected transcript: %q", out.String())
	}
}

func TestRunTaskReportsDeniedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "DENY", http.StatusForbidden)
	}))
	defer server.Close()

	err := runTask(context.Background(), server.Client(), server.URL, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected denied-request error, got %v", err)
	}
}
