package toolsearch

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello World! foo-bar_baz 123")
	if len(tokens) == 0 {
		t.Fatal("expected tokens")
	}
	// Should be lowercase and split on non-alnum
	for _, tok := range tokens {
		for _, r := range tok {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("token %q contains uppercase", tok)
			}
		}
	}
}

func TestNewIndex_Empty(t *testing.T) {
	idx := NewIndex(nil)
	results := idx.Search("anything", 5)
	if len(results) != 0 {
		t.Errorf("expected no results for empty index, got %d", len(results))
	}
}

func TestSearch_BasicRanking(t *testing.T) {
	tools := []ToolDoc{
		{Name: "list_dir", Description: "List directory contents recursively"},
		{Name: "read_file", Description: "Read a file from the filesystem"},
		{Name: "apply_patch", Description: "Apply a unified diff patch to files"},
		{Name: "shell_command", Description: "Execute a shell command in the terminal"},
	}

	idx := NewIndex(tools)

	// Search for "directory" should rank list_dir highest
	results := idx.Search("directory", 3)
	if len(results) == 0 {
		t.Fatal("expected results for 'directory'")
	}
	if results[0].Name != "list_dir" {
		t.Errorf("expected list_dir first, got %s", results[0].Name)
	}

	// Search for "file" should rank read_file or apply_patch high
	results = idx.Search("file", 3)
	if len(results) == 0 {
		t.Fatal("expected results for 'file'")
	}
	// Both read_file and apply_patch mention "file"
	found := false
	for _, r := range results {
		if r.Name == "read_file" || r.Name == "apply_patch" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected read_file or apply_patch in results for 'file'")
	}

	// Search for "shell command" should rank shell_command highest
	results = idx.Search("shell command", 3)
	if len(results) == 0 {
		t.Fatal("expected results for 'shell command'")
	}
	if results[0].Name != "shell_command" {
		t.Errorf("expected shell_command first, got %s", results[0].Name)
	}
}

func TestSearch_TopK(t *testing.T) {
	tools := []ToolDoc{
		{Name: "a", Description: "file operations"},
		{Name: "b", Description: "file reading"},
		{Name: "c", Description: "file writing"},
		{Name: "d", Description: "network operations"},
	}

	idx := NewIndex(tools)
	results := idx.Search("file", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	tools := []ToolDoc{
		{Name: "a", Description: "something"},
	}
	idx := NewIndex(tools)
	results := idx.Search("", 5)
	if len(results) != 0 {
		t.Errorf("expected no results for empty query, got %d", len(results))
	}
}

func TestSearch_Tags(t *testing.T) {
	tools := []ToolDoc{
		{Name: "deploy", Description: "Deploy application", Tags: []string{"kubernetes", "docker"}},
		{Name: "build", Description: "Build application", Tags: []string{"compile", "make"}},
	}

	idx := NewIndex(tools)
	results := idx.Search("kubernetes", 2)
	if len(results) == 0 {
		t.Fatal("expected results for 'kubernetes'")
	}
	if results[0].Name != "deploy" {
		t.Errorf("expected deploy first, got %s", results[0].Name)
	}
}
