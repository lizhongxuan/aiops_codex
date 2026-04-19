package filepatch

import (
	"testing"
)

func TestSafetyCheck_String(t *testing.T) {
	tests := []struct {
		check SafetyCheck
		want  string
	}{
		{SafetyAutoApprove, "auto_approve"},
		{SafetyAskUser, "ask_user"},
		{SafetyReject, "reject"},
		{SafetyCheck(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.check.String(); got != tt.want {
			t.Errorf("SafetyCheck(%d).String() = %q, want %q", tt.check, got, tt.want)
		}
	}
}

func TestAssessSafety_NilAction(t *testing.T) {
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"."}}
	check, err := AssessSafety(nil, policy, []string{"."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check != SafetyAutoApprove {
		t.Errorf("expected SafetyAutoApprove for nil action, got %v", check)
	}
}

func TestAssessSafety_EmptyChanges(t *testing.T) {
	action := &PatchAction{Changes: []FileChange{}}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"."}}
	check, err := AssessSafety(action, policy, []string{"."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check != SafetyAutoApprove {
		t.Errorf("expected SafetyAutoApprove for empty changes, got %v", check)
	}
}

func TestAssessSafety_FullAccessAutoApproves(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeCreate, NewPath: "/etc/passwd"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxFullAccess}
	check, err := AssessSafety(action, policy, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check != SafetyAutoApprove {
		t.Errorf("expected SafetyAutoApprove in full_access mode, got %v", check)
	}
}

func TestAssessSafety_ReadOnlyRejects(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeModify, NewPath: "src/main.go"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxReadOnly}
	check, err := AssessSafety(action, policy, []string{"."})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject in read_only mode, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error for read_only rejection")
	}
	safetyErr, ok := err.(*SafetyError)
	if !ok {
		t.Fatalf("expected *SafetyError, got %T", err)
	}
	if safetyErr.Path != "src/main.go" {
		t.Errorf("expected path 'src/main.go', got %q", safetyErr.Path)
	}
}

func TestAssessSafety_RejectsAbsolutePath(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeCreate, NewPath: "/etc/shadow"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"."}}
	check, err := AssessSafety(action, policy, []string{"."})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject for absolute path, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestAssessSafety_RejectsPathTraversal(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeModify, NewPath: "../../../etc/passwd"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"src"}}
	check, err := AssessSafety(action, policy, []string{"src"})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject for path traversal, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestAssessSafety_RejectsOutsideWritableRoots(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeCreate, NewPath: "config/secret.yaml"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"src"}}
	check, err := AssessSafety(action, policy, []string{"src"})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject for path outside writable roots, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error for path outside writable roots")
	}
}

func TestAssessSafety_ApproveWithinWritableRoots(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeCreate, NewPath: "src/handler.go"},
			{Mode: ModeModify, OldPath: "src/main.go", NewPath: "src/main.go"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"src"}}
	check, err := AssessSafety(action, policy, []string{"src"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check != SafetyAutoApprove {
		t.Errorf("expected SafetyAutoApprove for paths within writable roots, got %v", check)
	}
}

func TestAssessSafety_RenameChecksReadAndWrite(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeRename, OldPath: "docs/old.md", NewPath: "src/new.md"},
		},
	}
	// Writable: src only. Readable: docs.
	policy := SandboxPolicy{
		Mode:          SandboxWriteLocal,
		WritableRoots: []string{"src"},
		ReadableRoots: []string{"docs"},
	}
	// The rename source (docs/old.md) is readable, target (src/new.md) is writable.
	// But OldPath is also a write target for rename (it gets deleted).
	// docs/old.md is NOT in writable roots, so this should be rejected.
	check, err := AssessSafety(action, policy, []string{"src"})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject for rename with source outside writable roots, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAssessSafety_RenameWithinSameWritableRoot(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeRename, OldPath: "src/old.go", NewPath: "src/new.go"},
		},
	}
	policy := SandboxPolicy{
		Mode:          SandboxWriteLocal,
		WritableRoots: []string{"src"},
		ReadableRoots: []string{"src"},
	}
	check, err := AssessSafety(action, policy, []string{"src"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check != SafetyAutoApprove {
		t.Errorf("expected SafetyAutoApprove for rename within writable root, got %v", check)
	}
}

func TestAssessSafety_ProjectRootWritable(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeCreate, NewPath: "any/deep/path/file.go"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"."}}
	check, err := AssessSafety(action, policy, []string{"."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check != SafetyAutoApprove {
		t.Errorf("expected SafetyAutoApprove when project root is writable, got %v", check)
	}
}

func TestAssessSafety_RejectsPolicyWritableViolation(t *testing.T) {
	// writableRoots allows "src", but policy.WritableRoots only allows "src/internal"
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeModify, NewPath: "src/main.go"},
		},
	}
	policy := SandboxPolicy{
		Mode:          SandboxWriteLocal,
		WritableRoots: []string{"src/internal"},
	}
	check, err := AssessSafety(action, policy, []string{"src"})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject when policy writable roots are more restrictive, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error for policy violation")
	}
}

func TestAssessSafety_DeleteRequiresWritePermission(t *testing.T) {
	action := &PatchAction{
		Changes: []FileChange{
			{Mode: ModeDelete, OldPath: "vendor/lib.go"},
		},
	}
	policy := SandboxPolicy{Mode: SandboxWriteLocal, WritableRoots: []string{"src"}}
	check, err := AssessSafety(action, policy, []string{"src"})
	if check != SafetyReject {
		t.Errorf("expected SafetyReject for delete outside writable roots, got %v", check)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}
