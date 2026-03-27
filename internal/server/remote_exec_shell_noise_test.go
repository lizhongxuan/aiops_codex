package server

import "testing"

func TestExecResultCardStatusTreatsShellInitNoiseWithZeroExitCodeAsCompleted(t *testing.T) {
	result := remoteExecResult{
		Status:   "completed",
		ExitCode: 0,
		Stderr:   "/root/.bashrc: line 13: /.cargo/env: No such file or directory\n",
		Output:   "/root/.bashrc: line 13: /.cargo/env: No such file or directory\n",
	}

	if got := execResultCardStatus(result); got != "completed" {
		t.Fatalf("expected completed, got %q", got)
	}
}

func TestExecResultCardStatusKeepsNonZeroExitCodeFailed(t *testing.T) {
	result := remoteExecResult{
		Status:   "completed",
		ExitCode: 1,
		Stderr:   "/root/.bashrc: line 13: /.cargo/env: No such file or directory\n",
		Output:   "/root/.bashrc: line 13: /.cargo/env: No such file or directory\n",
	}

	if got := execResultCardStatus(result); got != "failed" {
		t.Fatalf("expected failed, got %q", got)
	}
}

func TestExecSuccessSummaryPrefersStdoutOverShellInitNoise(t *testing.T) {
	exec := &remoteExecSession{ToolName: "execute_readonly_query"}
	result := remoteExecResult{
		Status:   "completed",
		ExitCode: 0,
		Stdout:   "load average: 0.20 0.15 0.10\nusers: 3\n",
		Stderr:   "/root/.bashrc: line 13: /.cargo/env: No such file or directory\n",
		Output:   "load average: 0.20 0.15 0.10\nusers: 3\n/root/.bashrc: line 13: /.cargo/env: No such file or directory\n",
	}

	finalStatus := execResultCardStatus(result)
	if finalStatus != "completed" {
		t.Fatalf("expected completed, got %q", finalStatus)
	}

	summary, highlights, _ := buildExecCardPresentation(exec, result, finalStatus)
	if summary != "load average: 0.20 0.15 0.10" {
		t.Fatalf("expected stdout summary, got %q", summary)
	}
	if len(highlights) == 0 {
		t.Fatalf("expected highlights to be populated")
	}
	if highlights[0] != "users: 3" {
		t.Fatalf("expected secondary stdout highlight first, got %#v", highlights)
	}
}
