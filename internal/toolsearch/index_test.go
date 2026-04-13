package toolsearch

import (
	"testing"
)

func TestToolIndex_BuildAndSearch(t *testing.T) {
	ti := NewToolIndex()

	tools := []ToolEntry{
		{Name: "list_dir", Description: "List directory contents"},
		{Name: "read_file", Description: "Read file contents"},
		{Name: "write_file", Description: "Write file contents"},
	}

	ti.Build(tools)

	if ti.Size() != 3 {
		t.Errorf("expected size 3, got %d", ti.Size())
	}

	results := ti.Search("directory", 2)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Name != "list_dir" {
		t.Errorf("expected list_dir, got %s", results[0].Name)
	}
}

func TestToolIndex_Rebuild(t *testing.T) {
	ti := NewToolIndex()

	ti.Build([]ToolEntry{
		{Name: "old_tool", Description: "Old tool description"},
	})

	ti.Rebuild([]ToolEntry{
		{Name: "new_tool", Description: "New tool description"},
	})

	if ti.Size() != 1 {
		t.Errorf("expected size 1 after rebuild, got %d", ti.Size())
	}

	results := ti.Search("new", 1)
	if len(results) == 0 {
		t.Fatal("expected results after rebuild")
	}
	if results[0].Name != "new_tool" {
		t.Errorf("expected new_tool, got %s", results[0].Name)
	}
}

func TestToolIndex_EmptySearch(t *testing.T) {
	ti := NewToolIndex()
	results := ti.Search("anything", 5)
	if results != nil {
		t.Errorf("expected nil results for unbuilt index, got %v", results)
	}
}
