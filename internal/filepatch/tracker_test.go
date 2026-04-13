package filepatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobOID(t *testing.T) {
	// printf 'hello' | git hash-object --stdin => b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0
	data := []byte("hello")
	oid := blobOID(data)
	expected := "b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0"
	if oid != expected {
		t.Errorf("blobOID(%q) = %s, want %s", data, oid, expected)
	}
}

func TestBlobOIDEmpty(t *testing.T) {
	// Empty content: "blob 0\x00" => e69de29bb2d1d6434b8b29ae775ad8c2e48c5391
	oid := blobOID(nil)
	expected := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	if oid != expected {
		t.Errorf("blobOID(nil) = %s, want %s", oid, expected)
	}
}

func TestRecordBaseline_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := []byte("line1\nline2\n")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := NewTurnDiffTracker()
	if err := tracker.RecordBaseline(filePath); err != nil {
		t.Fatal(err)
	}

	baseline, ok := tracker.baselines[filePath]
	if !ok {
		t.Fatal("baseline not recorded")
	}
	if string(baseline.Content) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", baseline.Content, content)
	}
	if baseline.SHA1 == "" {
		t.Error("SHA1 should not be empty")
	}
}

func TestRecordBaseline_NonExistentFile(t *testing.T) {
	tracker := NewTurnDiffTracker()
	err := tracker.RecordBaseline("/nonexistent/path/file.txt")
	if err != nil {
		t.Fatal("should not error for non-existent file")
	}

	baseline, ok := tracker.baselines["/nonexistent/path/file.txt"]
	if !ok {
		t.Fatal("baseline not recorded for non-existent file")
	}
	if baseline.Content != nil {
		t.Error("content should be nil for non-existent file")
	}
	// Should have the empty blob OID
	if baseline.SHA1 != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Errorf("unexpected SHA1 for empty: %s", baseline.SHA1)
	}
}

func TestGenerateDiff_NoChanges(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "unchanged.txt")
	content := []byte("hello world\n")
	os.WriteFile(filePath, content, 0o644)

	tracker := NewTurnDiffTracker()
	tracker.RecordBaseline(filePath)

	diffs, err := tracker.GenerateDiff()
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for unchanged file, got %d", len(diffs))
	}
}

func TestGenerateDiff_ModifiedFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "modified.txt")
	original := []byte("line1\nline2\nline3\n")
	os.WriteFile(filePath, original, 0o644)

	tracker := NewTurnDiffTracker()
	tracker.RecordBaseline(filePath)

	// Modify the file
	modified := []byte("line1\nline2-changed\nline3\n")
	os.WriteFile(filePath, modified, 0o644)

	diffs, err := tracker.GenerateDiff()
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Path != filePath {
		t.Errorf("path = %s, want %s", d.Path, filePath)
	}
	if d.OldOID == d.NewOID {
		t.Error("OIDs should differ for modified file")
	}
	if !strings.Contains(d.Patch, "diff --git") {
		t.Error("patch should contain diff header")
	}
	if !strings.Contains(d.Patch, "-line2") {
		t.Error("patch should contain deleted line")
	}
	if !strings.Contains(d.Patch, "+line2-changed") {
		t.Error("patch should contain added line")
	}
}

func TestGenerateDiff_NewFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "newfile.txt")

	tracker := NewTurnDiffTracker()
	tracker.RecordBaseline(filePath) // file doesn't exist yet

	// Create the file
	os.WriteFile(filePath, []byte("new content\n"), 0o644)

	diffs, err := tracker.GenerateDiff()
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if !strings.Contains(d.Patch, "new file mode") {
		t.Error("patch should indicate new file")
	}
	if !strings.Contains(d.Patch, "+new content") {
		t.Error("patch should contain added content")
	}
}

func TestGenerateDiff_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "todelete.txt")
	os.WriteFile(filePath, []byte("goodbye\n"), 0o644)

	tracker := NewTurnDiffTracker()
	tracker.RecordBaseline(filePath)

	// Delete the file
	os.Remove(filePath)

	diffs, err := tracker.GenerateDiff()
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if !strings.Contains(d.Patch, "deleted file mode") {
		t.Error("patch should indicate deleted file")
	}
	if !strings.Contains(d.Patch, "-goodbye") {
		t.Error("patch should contain removed content")
	}
}

func TestGenerateDiff_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	file1 := filepath.Join(dir, "a.txt")
	file2 := filepath.Join(dir, "b.txt")
	os.WriteFile(file1, []byte("aaa\n"), 0o644)
	os.WriteFile(file2, []byte("bbb\n"), 0o644)

	tracker := NewTurnDiffTracker()
	tracker.RecordBaseline(file1)
	tracker.RecordBaseline(file2)

	// Modify both
	os.WriteFile(file1, []byte("aaa-modified\n"), 0o644)
	os.WriteFile(file2, []byte("bbb-modified\n"), 0o644)

	diffs, err := tracker.GenerateDiff()
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}

	// Should be sorted by path
	if diffs[0].Path > diffs[1].Path {
		t.Error("diffs should be sorted by path")
	}
}
