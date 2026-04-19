package agentloop

import (
	"os"
	"testing"
)

func TestCaptureSnapshot(t *testing.T) {
	snap, err := CaptureSnapshot()
	if err != nil {
		t.Fatalf("CaptureSnapshot failed: %v", err)
	}

	if snap.Cwd == "" {
		t.Error("expected non-empty Cwd")
	}

	if len(snap.Env) == 0 {
		t.Error("expected non-empty Env")
	}

	// PATH should be in env
	if _, ok := snap.Env["PATH"]; !ok {
		t.Error("expected PATH in captured env")
	}
}

func TestRestoreSnapshot_NilSnapshot(t *testing.T) {
	err := RestoreSnapshot(nil)
	if err == nil {
		t.Error("expected error for nil snapshot")
	}
}

func TestRestoreSnapshot_RestoresCwd(t *testing.T) {
	// Capture current state
	originalCwd, _ := os.Getwd()
	defer os.Chdir(originalCwd)

	snap := &ShellSnapshot{
		Cwd: originalCwd,
		Env: map[string]string{},
	}

	// Change to temp dir
	tmpDir := os.TempDir()
	os.Chdir(tmpDir)

	// Restore
	err := RestoreSnapshot(snap)
	if err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}

	cwd, _ := os.Getwd()
	if cwd != originalCwd {
		t.Errorf("expected cwd %s, got %s", originalCwd, cwd)
	}
}

func TestRestoreSnapshot_RestoresEnv(t *testing.T) {
	testKey := "AIOPS_TEST_SNAPSHOT_VAR"
	defer os.Unsetenv(testKey)

	snap := &ShellSnapshot{
		Cwd: "",
		Env: map[string]string{
			testKey: "restored_value",
		},
	}

	err := RestoreSnapshot(snap)
	if err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}

	val := os.Getenv(testKey)
	if val != "restored_value" {
		t.Errorf("expected env var %s=restored_value, got %s", testKey, val)
	}
}
