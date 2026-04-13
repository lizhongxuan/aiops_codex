package agentloop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestRegisterApplyPatchTool(t *testing.T) {
	reg := NewToolRegistry()
	RegisterApplyPatchTool(reg)

	entry, ok := reg.Get("apply_patch")
	if !ok {
		t.Fatal("expected apply_patch tool to be registered")
	}
	if entry.Name != "apply_patch" {
		t.Fatalf("expected name 'apply_patch', got %q", entry.Name)
	}
	if !entry.RequiresApproval {
		t.Fatal("apply_patch should require approval")
	}
	if entry.IsReadOnly {
		t.Fatal("apply_patch should not be read-only")
	}
	if entry.Handler == nil {
		t.Fatal("apply_patch should have a handler")
	}
}

func TestHandleApplyPatch_EmptyPatch(t *testing.T) {
	reg := NewToolRegistry()
	RegisterApplyPatchTool(reg)

	session := NewSession("test-patch", SessionSpec{Model: "test", Cwd: t.TempDir()})
	call := bifrost.ToolCall{ID: "call-1", Function: bifrost.FunctionCall{Name: "apply_patch"}}

	_, err := reg.Dispatch(context.Background(), session, call, "apply_patch", map[string]interface{}{
		"patch": "",
	})
	if err == nil {
		t.Fatal("expected error for empty patch")
	}
}

func TestHandleApplyPatch_CreateFile(t *testing.T) {
	tmpDir := t.TempDir()
	session := NewSession("test-patch-create", SessionSpec{Model: "test", Cwd: tmpDir})

	patch := `diff --git a/hello.txt b/hello.txt
new file mode 100644
--- /dev/null
+++ b/hello.txt
@@ -0,0 +1,2 @@
+Hello, World!
+Second line.
`
	call := bifrost.ToolCall{ID: "call-2", Function: bifrost.FunctionCall{Name: "apply_patch"}}
	result, err := handleApplyPatch(context.Background(), session, call, map[string]interface{}{
		"patch": patch,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Verify file was created.
	content, err := os.ReadFile(filepath.Join(tmpDir, "hello.txt"))
	if err != nil {
		t.Fatalf("failed to read created file: %v", err)
	}
	expected := "Hello, World!\nSecond line.\n"
	if string(content) != expected {
		t.Fatalf("file content mismatch:\ngot:  %q\nwant: %q", string(content), expected)
	}
}

func TestHandleApplyPatch_ModifyFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Create an existing file to modify.
	original := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "existing.txt"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	session := NewSession("test-patch-modify", SessionSpec{Model: "test", Cwd: tmpDir})

	patch := `diff --git a/existing.txt b/existing.txt
--- a/existing.txt
+++ b/existing.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2_modified
 line3
`
	call := bifrost.ToolCall{ID: "call-3", Function: bifrost.FunctionCall{Name: "apply_patch"}}
	result, err := handleApplyPatch(context.Background(), session, call, map[string]interface{}{
		"patch": patch,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "existing.txt"))
	if err != nil {
		t.Fatalf("failed to read modified file: %v", err)
	}
	expected := "line1\nline2_modified\nline3\n"
	if string(content) != expected {
		t.Fatalf("file content mismatch:\ngot:  %q\nwant: %q", string(content), expected)
	}
}

func TestHandleApplyPatch_DiffTrackerRecordsBaseline(t *testing.T) {
	tmpDir := t.TempDir()
	original := "original content\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "tracked.txt"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	session := NewSession("test-patch-tracker", SessionSpec{Model: "test", Cwd: tmpDir})
	tracker := session.DiffTracker()
	if tracker == nil {
		t.Fatal("expected non-nil diff tracker")
	}

	patch := `diff --git a/tracked.txt b/tracked.txt
--- a/tracked.txt
+++ b/tracked.txt
@@ -1 +1 @@
-original content
+modified content
`
	call := bifrost.ToolCall{ID: "call-4", Function: bifrost.FunctionCall{Name: "apply_patch"}}
	_, err := handleApplyPatch(context.Background(), session, call, map[string]interface{}{
		"patch": patch,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Generate diff from tracker — should show the change.
	diffs, err := tracker.GenerateDiff()
	if err != nil {
		t.Fatalf("GenerateDiff error: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff from tracker")
	}
}

func TestSessionCwdAccessor(t *testing.T) {
	s := NewSession("test-cwd", SessionSpec{Model: "test", Cwd: "/workspace/project"})
	if s.Cwd() != "/workspace/project" {
		t.Fatalf("expected Cwd '/workspace/project', got %q", s.Cwd())
	}
}

func TestSessionDiffTrackerInitialized(t *testing.T) {
	s := NewSession("test-tracker-init", SessionSpec{Model: "test"})
	if s.DiffTracker() == nil {
		t.Fatal("expected DiffTracker to be initialized")
	}
}
