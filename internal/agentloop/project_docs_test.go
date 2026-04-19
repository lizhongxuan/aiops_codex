package agentloop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDocInjector_Discover(t *testing.T) {
	// Create temp project with README
	tmpDir := t.TempDir()
	readmePath := filepath.Join(tmpDir, "README.md")
	os.WriteFile(readmePath, []byte("# Test Project"), 0644)

	injector := NewProjectDocInjector()
	docs := injector.Discover(tmpDir)

	if len(docs) == 0 {
		t.Fatal("expected to discover README.md")
	}

	found := false
	for _, d := range docs {
		if filepath.Base(d) == "README.md" {
			found = true
			break
		}
	}
	if !found {
		t.Error("README.md not found in discovered docs")
	}
}

func TestProjectDocInjector_Discover_NoDocs(t *testing.T) {
	tmpDir := t.TempDir()
	injector := NewProjectDocInjector()
	docs := injector.Discover(tmpDir)

	if len(docs) != 0 {
		t.Errorf("expected no docs, got %d", len(docs))
	}
}

func TestProjectDocInjector_Inject_TokenBudget(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a large README
	largeContent := make([]byte, 100000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	os.WriteFile(filepath.Join(tmpDir, "README.md"), largeContent, 0644)

	injector := &ProjectDocInjector{
		Paths:       []string{"README.md"},
		TokenBudget: 100, // Very small budget: ~400 chars
	}

	session := NewSession("test-session", SessionSpec{
		Cwd: tmpDir,
	})

	err := injector.Inject(session)
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	// Check that content was injected but truncated
	msgs := session.ContextManager().Messages()
	if len(msgs) == 0 {
		t.Fatal("expected injected message")
	}

	content, ok := msgs[0].Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if len(content) > 1000 {
		t.Errorf("content should be truncated by token budget, got %d chars", len(content))
	}
}

func TestProjectDocInjector_Inject_NilSession(t *testing.T) {
	injector := NewProjectDocInjector()
	err := injector.Inject(nil)
	if err == nil {
		t.Error("expected error for nil session")
	}
}
