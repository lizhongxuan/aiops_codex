package server

import (
	"strings"
	"testing"
)

func TestToolPromptDescriptionFallsBackToSharedVariant(t *testing.T) {
	for _, name := range []string{
		"web_search",
		"open_page",
		"find_in_page",
		"ask_user_question",
		"query_ai_server_state",
		"readonly_host_inspect",
		"enter_plan_mode",
		"update_plan",
		"exit_plan_mode",
		"orchestrator_dispatch_tasks",
		"request_approval",
		"apply_patch",
	} {
		shared := toolPromptDescription(name)
		if shared == "" {
			t.Fatalf("expected shared description for %s", name)
		}
		if got := localToolPromptDescription(name); got != shared {
			t.Fatalf("expected local fallback to shared description for %s, got %q want %q", name, got, shared)
		}
		if got := remoteToolPromptDescription(name); got != shared {
			t.Fatalf("expected remote fallback to shared description for %s, got %q want %q", name, got, shared)
		}
	}
}

func TestToolPromptDescriptionUsesLocalAndRemoteVariants(t *testing.T) {
	cases := []struct {
		name       string
		wantShared string
		wantLocal  string
		wantRemote string
	}{
		{
			name:       "execute_readonly_query",
			wantShared: "Run a read-only shell command for system inspection.",
			wantLocal:  `Always set host="server-local".`,
			wantRemote: "currently selected remote host",
		},
		{
			name:       "execute_system_mutation",
			wantShared: "Run a state-changing shell command or file change for controlled mutation.",
			wantLocal:  `Always set host="server-local".`,
			wantRemote: "currently selected remote host",
		},
		{
			name:       shellCommandToolName,
			wantShared: "Run a shell command through a single BashTool-style facade.",
			wantLocal:  `Always set host="server-local".`,
			wantRemote: "currently selected remote host",
		},
		{
			name:       "read_remote_file",
			wantShared: "Read a text file when you already know the path.",
			wantLocal:  "Read a text file when you already know the path.",
			wantRemote: "currently selected remote host",
		},
		{
			name:       "search_remote_files",
			wantShared: "Search for text within files under a known path.",
			wantLocal:  "Search for text within files under a known path.",
			wantRemote: "currently selected remote host",
		},
		{
			name:       "write_file",
			wantShared: "Write content to a file through a single FileWriteTool-style facade.",
			wantLocal:  "Write content to a file through a single FileWriteTool-style facade.",
			wantRemote: "currently selected remote host",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, ok := lookupToolPromptEntry(tc.name)
			if !ok {
				t.Fatalf("expected registry entry for %s", tc.name)
			}

			shared := toolPromptDescription(tc.name)
			if shared != entry.Description(toolPromptVariantShared) {
				t.Fatalf("shared helper should match registry source for %s, got %q want %q", tc.name, shared, entry.Description(toolPromptVariantShared))
			}
			if !strings.Contains(shared, tc.wantShared) {
				t.Fatalf("unexpected shared description for %s: %q", tc.name, shared)
			}

			local := localToolPromptDescription(tc.name)
			if local != entry.Description(toolPromptVariantLocal) {
				t.Fatalf("local helper should match registry source for %s, got %q want %q", tc.name, local, entry.Description(toolPromptVariantLocal))
			}
			if !strings.Contains(local, tc.wantLocal) {
				t.Fatalf("local description for %s should mention %q, got %q", tc.name, tc.wantLocal, local)
			}

			remote := remoteToolPromptDescription(tc.name)
			if remote != entry.Description(toolPromptVariantRemote) {
				t.Fatalf("remote helper should match registry source for %s, got %q want %q", tc.name, remote, entry.Description(toolPromptVariantRemote))
			}
			if !strings.Contains(remote, tc.wantRemote) {
				t.Fatalf("remote description for %s should mention %q, got %q", tc.name, tc.wantRemote, remote)
			}
		})
	}
}

func TestToolPromptRegistrySingleSourceBehavior(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks map[toolPromptVariant]func(string) string
	}{
		{
			name: "web_search",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "execute_readonly_query",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: shellCommandToolName,
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "read_remote_file",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "write_file",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "apply_patch",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "ask_user_question",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "enter_plan_mode",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "update_plan",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "exit_plan_mode",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
		{
			name: "request_approval",
			checks: map[toolPromptVariant]func(string) string{
				toolPromptVariantShared: toolPromptDescription,
				toolPromptVariantLocal:  localToolPromptDescription,
				toolPromptVariantRemote: remoteToolPromptDescription,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry, ok := lookupToolPromptEntry(tc.name)
			if !ok {
				t.Fatalf("expected registry entry for %s", tc.name)
			}

			for variant, helper := range tc.checks {
				got := helper(tc.name)
				want := entry.Description(variant)
				if got != want {
					t.Fatalf("helper and registry diverged for %s/%s: got %q want %q", tc.name, variant, got, want)
				}
			}
		})
	}
}
