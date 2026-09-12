package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestErrCategory(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("context deadline exceeded"), "Timeout"},
		{errors.New("connection refused"), "Connection refused"},
		{errors.New("connection reset by peer"), "Connection reset"},
		{errors.New("unexpected EOF"), "Unexpected EOF"},
		{errors.New("no such host: api.example.internal"), "DNS resolution"},
		{errors.New("certificate has expired"), "TLS/certificate"},
		{errors.New("write: broken pipe"), "Broken pipe"},
		{errors.New("context canceled"), "Canceled (test ended)"},
		{errors.New("too many open files"), "Resource exhaustion"},
		{errors.New("some arbitrary network glitch"), "Other"},
	}

	for _, tt := range tests {
		got := errCategory(tt.err)
		if got != tt.want {
			t.Errorf("errCategory(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestParseHeader(t *testing.T) {
	// Standard key-value
	k, v, err := parseHeader("Authorization: Bearer secret123")
	if err != nil || k != "Authorization" || v != "Bearer secret123" {
		t.Fatalf("unexpected result: %q, %q, %v", k, v, err)
	}

	// Whitespace trimming
	k, v, err = parseHeader("   X-Custom-Header  :   some value   ")
	if err != nil || k != "X-Custom-Header" || v != "some value" {
		t.Fatalf("unexpected whitespace trimming: %q, %q, %v", k, v, err)
	}

	// Missing colon
	_, _, err = parseHeader("InvalidHeaderNoColon")
	if err == nil {
		t.Fatal("expected error for header without colon, got nil")
	}

	// @file header loading
	tmpDir := t.TempDir()
	hdrFile := filepath.Join(tmpDir, "jwt.txt")
	if err := os.WriteFile(hdrFile, []byte("Authorization: Bearer from-file\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, v, err = parseHeader("@" + hdrFile)
	if err != nil || k != "Authorization" || v != "Bearer from-file" {
		t.Fatalf("unexpected @file result: %q, %q, %v", k, v, err)
	}

	// @file not found
	_, _, err = parseHeader("@/nonexistent/file/for/sure.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent @file, got nil")
	}
}

func TestPercentileAndAvg(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}

	p50 := percentile(vals, 50)
	if p50 != 50 {
		t.Errorf("p50 = %v, want 50", p50)
	}

	p90 := percentile(vals, 90)
	if p90 != 90 {
		t.Errorf("p90 = %v, want 90", p90)
	}

	a := avg(vals)
	if a != 55 {
		t.Errorf("avg = %v, want 55", a)
	}

	if avg(nil) != 0 {
		t.Errorf("avg(nil) = %v, want 0", avg(nil))
	}
}

func TestComputeStats(t *testing.T) {
	results := []result{
		{status: 200, latency: 10 * time.Millisecond},
		{status: 200, latency: 20 * time.Millisecond},
		{status: 500, latency: 30 * time.Millisecond},
		{err: errors.New("timeout: deadline exceeded")},
	}

	stats := computeStats(results, 1*time.Second, 4, 2)
	if stats.totalReq != 4 {
		t.Errorf("totalReq = %d, want 4", stats.totalReq)
	}
	if stats.ok != 3 {
		t.Errorf("ok = %d, want 3", stats.ok)
	}
	if stats.errs != 1 {
		t.Errorf("errs = %d, want 1", stats.errs)
	}
	if stats.errRate != 25.0 {
		t.Errorf("errRate = %v, want 25.0", stats.errRate)
	}
	if stats.statusCounts[200] != 2 {
		t.Errorf("status 200 count = %d, want 2", stats.statusCounts[200])
	}
	if stats.statusCounts[500] != 1 {
		t.Errorf("status 500 count = %d, want 1", stats.statusCounts[500])
	}
	if stats.errCats["Timeout"] != 1 {
		t.Errorf("Timeout category count = %d, want 1", stats.errCats["Timeout"])
	}
}

func TestPresets(t *testing.T) {
	expected := []string{"baseline", "realistic", "capacity", "spike"}
	for _, name := range expected {
		p, ok := presets[name]
		if !ok {
			t.Errorf("preset %q missing", name)
		}
		if p.concurrency <= 0 {
			t.Errorf("preset %q concurrency must be > 0, got %d", name, p.concurrency)
		}
	}
}

func TestWorkerExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "val" {
			t.Errorf("missing header X-Test")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "ping" {
			t.Errorf("unexpected body: %q", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	baseReq, err := http.NewRequest("POST", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseReq.Header.Set("X-Test", "val")
	baseReq.ContentLength = int64(len("ping"))

	client := server.Client()
	ctx := withLimit(context.Background(), 10)

	var (
		results   []result
		resultsMu sync.Mutex
		wg        sync.WaitGroup
		sent      atomic.Int64
	)

	wg.Add(2)
	go worker(ctx, client, baseReq, []byte("ping"), nil, 0, &results, &resultsMu, &wg, &sent)
	go worker(ctx, client, baseReq, []byte("ping"), nil, 0, &results, &resultsMu, &wg, &sent)

	wg.Wait()

	if len(results) < 10 {
		t.Errorf("completed requests = %d, want at least 10", len(results))
	}
	for _, r := range results {
		if r.err != nil {
			t.Errorf("unexpected worker error: %v", r.err)
		}
		if r.status != 200 {
			t.Errorf("unexpected status: %d", r.status)
		}
	}
}
