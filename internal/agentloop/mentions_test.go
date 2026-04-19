package agentloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveMentions_FileReference(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(testFile, []byte("package main\nfunc main() {}"), 0644)

	session := NewSession("test", SessionSpec{Cwd: tmpDir})

	input := "Look at @test.go for the implementation"
	result, err := ResolveMentions(input, session)
	if err != nil {
		t.Fatalf("ResolveMentions failed: %v", err)
	}

	if !strings.Contains(result, "package main") {
		t.Error("expected file content to be injected")
	}
	if !strings.Contains(result, "@test.go") {
		t.Error("expected original mention to be preserved")
	}
}

func TestResolveMentions_AgentReference(t *testing.T) {
	session := NewSession("test", SessionSpec{Cwd: "/nonexistent"})

	input := "Ask @code-reviewer to check this"
	result, err := ResolveMentions(input, session)
	if err != nil {
		t.Fatalf("ResolveMentions failed: %v", err)
	}

	if !strings.Contains(result, "routed to agent: code-reviewer") {
		t.Error("expected agent routing message")
	}
}

func TestResolveMentions_NoMentions(t *testing.T) {
	session := NewSession("test", SessionSpec{Cwd: "/"})

	input := "No mentions here"
	result, err := ResolveMentions(input, session)
	if err != nil {
		t.Fatalf("ResolveMentions failed: %v", err)
	}

	if result != input {
		t.Errorf("expected unchanged input, got: %s", result)
	}
}

func TestResolveMentions_NilSession(t *testing.T) {
	input := "Look at @file.go"
	result, err := ResolveMentions(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != input {
		t.Error("expected unchanged input for nil session")
	}
}

func TestResolveMentions_MultipleMentions(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("file a"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("file b"), 0644)

	session := NewSession("test", SessionSpec{Cwd: tmpDir})

	input := "Compare @a.go and @b.go"
	result, err := ResolveMentions(input, session)
	if err != nil {
		t.Fatalf("ResolveMentions failed: %v", err)
	}

	if !strings.Contains(result, "file a") {
		t.Error("expected content of a.go")
	}
	if !strings.Contains(result, "file b") {
		t.Error("expected content of b.go")
	}
}
