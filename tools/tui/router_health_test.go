package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type routeTransport func(*http.Request) (*http.Response, error)

func (f routeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRouteHealthPreflight(t *testing.T) {
	old := routeHTTPClient
	t.Cleanup(func() { routeHTTPClient = old })
	routeHTTPClient = &http.Client{Transport: routeTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/tags" {
			t.Fatal(req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	if err := routeHealthPreflight(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRouteTimeoutKillsDelegation(t *testing.T) {
	old := routeHTTPClient
	t.Cleanup(func() { routeHTTPClient = old })
	routeHTTPClient = &http.Client{Transport: routeTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tools", "automation"), 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "tools", "automation", "ollama-delegate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30 &\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := defaultLocalRoute(ctx, dir, "commit-msg", "private prompt")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(start))
	}
}

func TestLocalRouteSuccessAndFailedOutputBuffer(t *testing.T) {
	oldClient, oldOut := routeHTTPClient, os.Stdout
	t.Cleanup(func() { routeHTTPClient = oldClient; os.Stdout = oldOut })
	routeHTTPClient = &http.Client{Transport: routeTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	dir := t.TempDir()
	fake := filepath.Join(dir, "bash")
	// Real subprocess, injected HTTP health. No listener is needed.
	body := "#!/bin/sh\n[ \"$2\" = commit-msg ] || exit 1\n[ \"$(cat \"$3\")\" = 'private prompt' ] || exit 1\nprintf 'provider-output'\nexit ${STUB_EXIT:-0}\n"
	if err := os.WriteFile(fake, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	capture, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	os.Stdout = capture
	if err := defaultLocalRoute(context.Background(), dir, "commit-msg", "private prompt"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(capture.Name())
	if string(data) != "provider-output" {
		t.Fatalf("output=%q", data)
	}
	t.Setenv("STUB_EXIT", "3")
	if err := defaultLocalRoute(context.Background(), dir, "commit-msg", "private prompt"); err == nil {
		t.Fatal("accepted failed subprocess")
	}
	data, _ = os.ReadFile(capture.Name())
	if string(data) != "provider-output" {
		t.Fatalf("failed output leaked: %q", data)
	}
}
