package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// bearerGH puts a gh on PATH that answers the token lookup with token, or
// nothing when it exits 1.
func bearerGH(t *testing.T, script string) {
	t.Helper()

	bin := t.TempDir()

	//nolint:gosec // the stub gh on PATH has to be executable
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o750); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

//nolint:paralleltest // the stub gh is put on PATH for the whole process
func TestFetchSendsTheURLHostsToken(t *testing.T) {
	bearerGH(t, "#!/bin/sh\necho stub-token\n")

	var auth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if auth != "Bearer stub-token" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		if _, err := io.WriteString(w, "png-bytes"); err != nil {
			t.Errorf("writing the answer: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	path, err := fetch(context.Background(), srv.URL+"/shot.png")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Errorf("removing the download: %v", err)
		}
	})

	data, err := os.ReadFile(path) //nolint:gosec // the path is fetch's own temp file
	if err != nil {
		t.Fatalf("reading the download: %v", err)
	}
	if string(data) != "png-bytes" {
		t.Errorf("downloaded %q, want png-bytes", data)
	}
	if auth != "Bearer stub-token" {
		t.Errorf("authorization = %q, want the host's token", auth)
	}
}

//nolint:paralleltest // the stub gh is put on PATH for the whole process
func TestFetchWithoutATokenFetchesAnonymously(t *testing.T) {
	bearerGH(t, "#!/bin/sh\nexit 1\n")

	var auth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")

		if _, err := io.WriteString(w, "png-bytes"); err != nil {
			t.Errorf("writing the answer: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	path, err := fetch(context.Background(), srv.URL+"/shot.png")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Errorf("removing the download: %v", err)
		}
	})

	if auth != "" {
		t.Errorf("a host gh holds no token for was sent %q", auth)
	}
}
