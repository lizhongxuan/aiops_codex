package filepatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollback_RestoresModifiedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	original := []byte("original content\n")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate a modification
	if err := os.WriteFile(target, []byte("modified content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	backups := map[string][]byte{
		"file.txt": original,
	}

	if err := Rollback(nil, dir, backups); err != nil {
		t.Fatalf("rollback error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Errorf("expected %q, got %q", string(original), string(data))
	}
}

func TestRollback_RemovesCreatedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(target, []byte("created\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// nil backup means file didn't exist before — should be removed
	backups := map[string][]byte{
		"new.txt": nil,
	}

	if err := Rollback(nil, dir, backups); err != nil {
		t.Fatalf("rollback error: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected file to be removed after rollback")
	}
}

func TestRollback_RestoresDeletedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "deleted.txt")
	original := []byte("should be restored\n")

	// File was deleted during patch
	backups := map[string][]byte{
		"deleted.txt": original,
	}

	if err := Rollback(nil, dir, backups); err != nil {
		t.Fatalf("rollback error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Errorf("expected %q, got %q", string(original), string(data))
	}
}

func TestApply_RollbackOnPartialFailure(t *testing.T) {
	dir := t.TempDir()

	// Create first file that will be successfully modified
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create second file with content that won't match the diff (will cause failure)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("wrong content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,1 +1,1 @@
-aaa
+bbb
diff --git a/b.txt b/b.txt
--- a/b.txt
+++ b/b.txt
@@ -1,1 +1,1 @@
-expected line
+replacement
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = Apply(action, dir)
	if err == nil {
		t.Fatal("expected error from second change")
	}
	if !strings.Contains(err.Error(), "baseline validation failed") {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify first file was rolled back to original content
	data, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "aaa\n" {
		t.Errorf("expected a.txt to be rolled back, got %q", string(data))
	}
}

func TestApply_RollbackOnCreateThenFailure(t *testing.T) {
	dir := t.TempDir()

	// Second file has wrong content to trigger failure
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("wrong\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := `diff --git a/brand-new.txt b/brand-new.txt
new file mode 100644
--- /dev/null
+++ b/brand-new.txt
@@ -0,0 +1,1 @@
+hello
diff --git a/existing.txt b/existing.txt
--- a/existing.txt
+++ b/existing.txt
@@ -1,1 +1,1 @@
-expected
+replaced
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = Apply(action, dir)
	if err == nil {
		t.Fatal("expected error")
	}

	// The created file should be removed by rollback
	if _, err := os.Stat(filepath.Join(dir, "brand-new.txt")); !os.IsNotExist(err) {
		t.Error("expected brand-new.txt to be removed after rollback")
	}
}
