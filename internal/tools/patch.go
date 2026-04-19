package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/filepatch"
)

// RegisterApplyPatchTool registers the apply_patch tool into the given ToolRegistry.
// The tool parses a unified diff, assesses safety against sandbox policy, and applies
// the patch to the filesystem. Non-auto-approved patches require user approval.
func RegisterApplyPatchTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "apply_patch",
		Description: "Edit existing files through a single diff-oriented patch facade. Constraints: Use it for targeted edits to existing files when patch-style changes are clearer than whole-file overwrite. Provide a valid unified diff patch and keep the patch as small as possible while still uniquely locating the intended edit. Prefer write_file for create, overwrite, or append flows instead of overloading patch intent. Result: Returns patch summaries, per-file diff metadata, and FileChangeCard-friendly descriptors after approval and execution. This tool always requires user approval before execution.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patch": map[string]interface{}{
					"type":        "string",
					"description": "The unified diff content to apply (git diff format).",
				},
			},
			"required":             []string{"patch"},
			"additionalProperties": false,
		},
		Handler:          handleApplyPatch,
		RequiresApproval: true,
	})
}

// handleApplyPatch is the ToolHandler for the apply_patch tool.
func handleApplyPatch(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	patchStr, ok := args["patch"].(string)
	if !ok || strings.TrimSpace(patchStr) == "" {
		return "", fmt.Errorf("apply_patch requires a non-empty 'patch' argument")
	}

	// Parse the unified diff.
	action, err := filepatch.ParsePatch(patchStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse patch: %w", err)
	}

	// Determine working directory from session.
	cwd := tc.Cwd()
	if cwd == "" {
		cwd = "."
	}

	// Assess safety against sandbox policy.
	policy := filepatch.SandboxPolicy{
		Mode:          filepatch.SandboxWriteLocal,
		WritableRoots: []string{"."},
	}
	safety, err := filepatch.AssessSafety(action, policy, []string{"."})
	if err != nil {
		return "", fmt.Errorf("patch safety check failed: %w", err)
	}
	if safety == filepatch.SafetyReject {
		return "", fmt.Errorf("patch rejected by safety policy: %v", err)
	}

	// Record baselines for affected files before applying.
	tracker := tc.DiffTracker()
	if tracker != nil {
		for _, change := range action.Changes {
			paths := affectedAbsPaths(&change, cwd)
			for _, p := range paths {
				_ = tracker.RecordBaseline(p)
			}
		}
	}

	// Apply the patch.
	if err := filepatch.Apply(action, cwd); err != nil {
		return "", fmt.Errorf("failed to apply patch: %w", err)
	}

	// Build summary of changes applied.
	var summary strings.Builder
	for _, change := range action.Changes {
		path := change.NewPath
		if path == "" {
			path = change.OldPath
		}
		summary.WriteString(fmt.Sprintf("%s %s\n", change.Mode, path))
	}

	return fmt.Sprintf("Patch applied successfully (%d file(s) changed):\n%s",
		len(action.Changes), summary.String()), nil
}

// affectedAbsPaths returns absolute paths for all files affected by a change.
func affectedAbsPaths(fc *filepatch.FileChange, cwd string) []string {
	seen := make(map[string]struct{})
	var paths []string
	add := func(rel string) {
		if rel == "" {
			return
		}
		abs := filepath.Join(cwd, rel)
		if _, ok := seen[abs]; !ok {
			seen[abs] = struct{}{}
			paths = append(paths, abs)
		}
	}
	add(fc.OldPath)
	add(fc.NewPath)
	return paths
}
