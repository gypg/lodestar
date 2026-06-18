package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lingyuins/octopus/internal/conf"
)

func TestResolveLocalStaticDirPrefersWebOutInDebug(t *testing.T) {
	t.Setenv("GGZERO_DEBUG", "true")

	root := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	mustWriteStaticIndex(t, filepath.Join(root, "web", "out", "index.html"))
	mustWriteStaticIndex(t, filepath.Join(root, "static", "out", "index.html"))

	got, ok := resolveLocalStaticDir()
	if !ok {
		t.Fatalf("expected local static dir")
	}
	if filepath.Clean(got) != filepath.Clean(filepath.Join("web", "out")) {
		t.Fatalf("expected web/out, got %q", got)
	}
	if !conf.IsDebug() {
		t.Fatalf("expected debug mode from test env")
	}
}

func TestResolveLocalStaticDirFallsBackToStaticOutInDebug(t *testing.T) {
	t.Setenv("GGZERO_DEBUG", "true")

	root := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	mustWriteStaticIndex(t, filepath.Join(root, "static", "out", "index.html"))

	got, ok := resolveLocalStaticDir()
	if !ok {
		t.Fatalf("expected local static dir")
	}
	if filepath.Clean(got) != filepath.Clean(filepath.Join("static", "out")) {
		t.Fatalf("expected static/out, got %q", got)
	}
}

func TestResolveLocalStaticDirDisabledOutsideDebug(t *testing.T) {
	t.Setenv("GGZERO_DEBUG", "false")

	root := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	mustWriteStaticIndex(t, filepath.Join(root, "web", "out", "index.html"))

	if got, ok := resolveLocalStaticDir(); ok || got != "" {
		t.Fatalf("expected no local static dir outside debug, got %q ok=%v", got, ok)
	}
}

func mustWriteStaticIndex(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
