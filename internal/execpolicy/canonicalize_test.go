package execpolicy

import (
	"testing"
)

func TestCanonicalizeCommand(t *testing.T) {
	shell := ShellConfig{Type: "bash", QuoteChar: "'", PathSep: "/"}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"trim_whitespace", "  ls -la  ", "ls -la"},
		{"normalize_spaces", "ls   -la    /tmp", "ls -la /tmp"},
		{"resolve_alias_ll", "ll /tmp", "ls -l /tmp"},
		{"resolve_alias_la", "la", "ls -la"},
		{"trailing_semicolons", "echo hello;", "echo hello"},
		{"tabs_to_spaces", "cat\t\tfile.txt", "cat file.txt"},
		{"no_change", "grep -r pattern .", "grep -r pattern ."},
		{"resolve_alias_cls", "cls", "clear"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeCommand(tt.input, shell)
			if got != tt.expected {
				t.Errorf("CanonicalizeCommand(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCanonicalizeCommand_Windows(t *testing.T) {
	shell := ShellConfig{Type: "powershell", QuoteChar: "\"", PathSep: "\\"}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"basic", "dir C:\\Users", "dir C:\\Users"},
		{"trim", "  Get-Process  ", "Get-Process"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeCommand(tt.input, shell)
			if got != tt.expected {
				t.Errorf("CanonicalizeCommand(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello  world", "hello world"},
		{"a\t\tb", "a b"},
		{"  leading", " leading"},
		{"trailing  ", "trailing "},
		{"no change", "no change"},
	}

	for _, tt := range tests {
		got := normalizeWhitespace(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeWhitespace(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
