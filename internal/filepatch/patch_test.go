package filepatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePatch_EmptyInput(t *testing.T) {
	action, err := ParsePatch("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(action.Changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(action.Changes))
	}
}

func TestParsePatch_SingleFileModify(t *testing.T) {
	diff := `diff --git a/hello.txt b/hello.txt
index abc1234..def5678 100644
--- a/hello.txt
+++ b/hello.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2modified
 line3
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(action.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(action.Changes))
	}
	fc := action.Changes[0]
	if fc.Mode != ModeModify {
		t.Errorf("expected mode modify, got %s", fc.Mode)
	}
	if fc.OldPath != "hello.txt" {
		t.Errorf("expected OldPath hello.txt, got %s", fc.OldPath)
	}
	if len(fc.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(fc.Hunks))
	}
	h := fc.Hunks[0]
	if h.OldStart != 1 || h.OldCount != 3 || h.NewStart != 1 || h.NewCount != 3 {
		t.Errorf("unexpected hunk header: %+v", h)
	}
}

func TestParsePatch_NewFile(t *testing.T) {
	diff := `diff --git a/new.txt b/new.txt
new file mode 100644
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fc := action.Changes[0]
	if fc.Mode != ModeCreate {
		t.Errorf("expected mode create, got %s", fc.Mode)
	}
	if fc.NewPath != "new.txt" {
		t.Errorf("expected NewPath new.txt, got %s", fc.NewPath)
	}
}

func TestParsePatch_DeleteFile(t *testing.T) {
	diff := `diff --git a/old.txt b/old.txt
deleted file mode 100644
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-goodbye
-world
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fc := action.Changes[0]
	if fc.Mode != ModeDelete {
		t.Errorf("expected mode delete, got %s", fc.Mode)
	}
}

func TestParsePatch_RenameFile(t *testing.T) {
	diff := `diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fc := action.Changes[0]
	if fc.Mode != ModeRename {
		t.Errorf("expected mode rename, got %s", fc.Mode)
	}
	if fc.OldPath != "old.txt" {
		t.Errorf("expected OldPath old.txt, got %s", fc.OldPath)
	}
	if fc.NewPath != "new.txt" {
		t.Errorf("expected NewPath new.txt, got %s", fc.NewPath)
	}
}

func TestParsePatch_MalformedInput(t *testing.T) {
	_, err := ParsePatch("this is not a diff")
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
}

func TestParsePatch_MultipleFiles(t *testing.T) {
	diff := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,1 +1,1 @@
-old
+new
diff --git a/b.txt b/b.txt
--- a/b.txt
+++ b/b.txt
@@ -1,1 +1,1 @@
-foo
+bar
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(action.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(action.Changes))
	}
}

func TestApply_CreateFile(t *testing.T) {
	dir := t.TempDir()
	diff := `diff --git a/newfile.txt b/newfile.txt
new file mode 100644
--- /dev/null
+++ b/newfile.txt
@@ -0,0 +1,3 @@
+line1
+line2
+line3
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := Apply(action, dir); err != nil {
		t.Fatalf("apply error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "newfile.txt"))
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	expected := "line1\nline2\nline3\n"
	if string(content) != expected {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(content), expected)
	}
}

func TestApply_ModifyFile(t *testing.T) {
	dir := t.TempDir()
	original := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := `diff --git a/test.txt b/test.txt
--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2modified
 line3
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := Apply(action, dir); err != nil {
		t.Fatalf("apply error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "line1\nline2modified\nline3\n"
	if string(content) != expected {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(content), expected)
	}
}

func TestApply_DeleteFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "del.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := `diff --git a/del.txt b/del.txt
deleted file mode 100644
--- a/del.txt
+++ /dev/null
@@ -1,1 +0,0 @@
-bye
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := Apply(action, dir); err != nil {
		t.Fatalf("apply error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "del.txt")); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestApply_RenameFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := `diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := Apply(action, dir); err != nil {
		t.Fatalf("apply error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Error("expected old file to not exist")
	}
	content, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content\n" {
		t.Errorf("unexpected content: %q", string(content))
	}
}

func TestApply_BaselineValidationFails(t *testing.T) {
	dir := t.TempDir()
	// Write content that doesn't match what the diff expects
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := `diff --git a/test.txt b/test.txt
--- a/test.txt
+++ b/test.txt
@@ -1,1 +1,1 @@
-expected
+replacement
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	err = Apply(action, dir)
	if err == nil {
		t.Fatal("expected baseline validation error")
	}
	if !strings.Contains(err.Error(), "baseline validation failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestApply_CreateInSubdirectory(t *testing.T) {
	dir := t.TempDir()
	diff := `diff --git a/sub/dir/file.txt b/sub/dir/file.txt
new file mode 100644
--- /dev/null
+++ b/sub/dir/file.txt
@@ -0,0 +1,1 @@
+nested content
`
	action, err := ParsePatch(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := Apply(action, dir); err != nil {
		t.Fatalf("apply error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "sub", "dir", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "nested content\n" {
		t.Errorf("unexpected content: %q", string(content))
	}
}
