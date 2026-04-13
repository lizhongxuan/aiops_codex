package agentloop

import (
	"os"
	"runtime"
	"testing"
)

func TestDetectShell(t *testing.T) {
	config := DetectShell()

	if config.Type == "" {
		t.Error("DetectShell should return a non-empty type")
	}
	if config.PathSep == "" {
		t.Error("DetectShell should return a non-empty path separator")
	}
	if config.QuoteChar == "" {
		t.Error("DetectShell should return a non-empty quote char")
	}

	if runtime.GOOS != "windows" {
		if config.PathSep != "/" {
			t.Errorf("expected / path separator on unix, got %s", config.PathSep)
		}
	}
}

func TestDetectShell_Bash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	orig := os.Getenv("SHELL")
	os.Setenv("SHELL", "/bin/bash")
	defer os.Setenv("SHELL", orig)

	config := DetectShell()
	if config.Type != ShellBash {
		t.Errorf("expected bash, got %s", config.Type)
	}
	if len(config.Flags) == 0 || config.Flags[0] != "-c" {
		t.Error("bash should have -c flag")
	}
}

func TestDetectShell_Zsh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	orig := os.Getenv("SHELL")
	os.Setenv("SHELL", "/bin/zsh")
	defer os.Setenv("SHELL", orig)

	config := DetectShell()
	if config.Type != ShellZsh {
		t.Errorf("expected zsh, got %s", config.Type)
	}
}

func TestDetectShell_NoShellEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	orig := os.Getenv("SHELL")
	os.Unsetenv("SHELL")
	defer os.Setenv("SHELL", orig)

	config := DetectShell()
	if config.Type != ShellSh {
		t.Errorf("expected sh fallback, got %s", config.Type)
	}
}
