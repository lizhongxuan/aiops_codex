package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAgentTerminalCwdAbsolutePathSkipsHomeLookup(t *testing.T) {
	dir := t.TempDir()

	original := agentUserHomeDir
	called := false
	agentUserHomeDir = func() (string, error) {
		called = true
		return "", errors.New("unexpected home lookup")
	}
	t.Cleanup(func() {
		agentUserHomeDir = original
	})

	cwd, err := resolveAgentTerminalCwd(dir)
	if err != nil {
		t.Fatalf("resolveAgentTerminalCwd: %v", err)
	}
	if cwd != dir {
		t.Fatalf("expected %q, got %q", dir, cwd)
	}
	if called {
		t.Fatalf("expected absolute cwd to bypass home lookup")
	}
}

func TestResolveAgentFilePathAbsolutePathSkipsHomeLookup(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "os-release")
	if err := os.WriteFile(file, []byte("NAME=test\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	original := agentUserHomeDir
	called := false
	agentUserHomeDir = func() (string, error) {
		called = true
		return "", errors.New("unexpected home lookup")
	}
	t.Cleanup(func() {
		agentUserHomeDir = original
	})

	path, err := resolveAgentFilePath(file)
	if err != nil {
		t.Fatalf("resolveAgentFilePath: %v", err)
	}
	if path != file {
		t.Fatalf("expected %q, got %q", file, path)
	}
	if called {
		t.Fatalf("expected absolute file path to bypass home lookup")
	}
}
