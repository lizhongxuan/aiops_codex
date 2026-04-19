// Package filepatch implements a unified diff parser and applier.
package filepatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiffOp represents the operation type for a diff line.
type DiffOp byte

const (
	OpContext DiffOp = ' '
	OpAdd     DiffOp = '+'
	OpDelete  DiffOp = '-'
)

// DiffLine represents a single line in a hunk.
type DiffLine struct {
	Op      DiffOp
	Content string
}

// Hunk represents a contiguous block of changes within a file.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []DiffLine
}

// ChangeMode describes what kind of change is being made to a file.
type ChangeMode string

const (
	ModeCreate ChangeMode = "create"
	ModeModify ChangeMode = "modify"
	ModeDelete ChangeMode = "delete"
	ModeRename ChangeMode = "rename"
)

// FileChange represents the set of changes to a single file.
type FileChange struct {
	Mode    ChangeMode
	OldPath string
	NewPath string
	Hunks   []Hunk
}

// PatchAction holds the complete set of file changes parsed from a unified diff.
type PatchAction struct {
	Changes []FileChange
}

// ParsePatch parses a unified diff string into a PatchAction.
func ParsePatch(diff string) (*PatchAction, error) {
	if strings.TrimSpace(diff) == "" {
		return &PatchAction{}, nil
	}

	lines := strings.Split(diff, "\n")
	action := &PatchAction{}
	i := 0

	for i < len(lines) {
		// Skip empty lines between file diffs
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}

		// Look for diff header
		if !strings.HasPrefix(lines[i], "diff --git ") {
			// Try to find next diff header
			i++
			continue
		}

		fc, nextIdx, err := parseFileChange(lines, i)
		if err != nil {
			return nil, err
		}
		action.Changes = append(action.Changes, *fc)
		i = nextIdx
	}

	if len(action.Changes) == 0 {
		return nil, fmt.Errorf("no valid diff sections found in input")
	}

	return action, nil
}

func parseFileChange(lines []string, start int) (*FileChange, int, error) {
	fc := &FileChange{Mode: ModeModify}
	i := start

	// Parse "diff --git a/path b/path"
	diffLine := lines[i]
	parts := parseDiffGitLine(diffLine)
	if parts == nil {
		return nil, 0, fmt.Errorf("malformed diff header at line %d: %q", i+1, diffLine)
	}
	fc.OldPath = parts[0]
	fc.NewPath = parts[1]
	i++

	// Parse extended headers (new file mode, deleted file mode, rename, etc.)
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "new file mode") {
			fc.Mode = ModeCreate
			i++
		} else if strings.HasPrefix(line, "deleted file mode") {
			fc.Mode = ModeDelete
			i++
		} else if strings.HasPrefix(line, "rename from ") {
			fc.Mode = ModeRename
			fc.OldPath = strings.TrimPrefix(line, "rename from ")
			i++
		} else if strings.HasPrefix(line, "rename to ") {
			fc.Mode = ModeRename
			fc.NewPath = strings.TrimPrefix(line, "rename to ")
			i++
		} else if strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "dissimilarity index") ||
			strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") {
			i++
		} else {
			break
		}
	}

	// Parse --- and +++ lines
	if i < len(lines) && strings.HasPrefix(lines[i], "--- ") {
		oldPath := strings.TrimPrefix(lines[i], "--- ")
		if oldPath != "/dev/null" {
			oldPath = strings.TrimPrefix(oldPath, "a/")
			if fc.Mode != ModeRename {
				fc.OldPath = oldPath
			}
		}
		i++
	}
	if i < len(lines) && strings.HasPrefix(lines[i], "+++ ") {
		newPath := strings.TrimPrefix(lines[i], "+++ ")
		if newPath != "/dev/null" {
			newPath = strings.TrimPrefix(newPath, "b/")
			if fc.Mode != ModeRename {
				fc.NewPath = newPath
			}
		}
		i++
	}

	// Parse hunks
	for i < len(lines) {
		if strings.HasPrefix(lines[i], "diff --git ") {
			break
		}
		if strings.HasPrefix(lines[i], "@@ ") {
			hunk, nextIdx, err := parseHunk(lines, i)
			if err != nil {
				return nil, 0, err
			}
			fc.Hunks = append(fc.Hunks, *hunk)
			i = nextIdx
		} else {
			i++
		}
	}

	return fc, i, nil
}

func parseHunk(lines []string, start int) (*Hunk, int, error) {
	header := lines[start]
	hunk := &Hunk{}

	// Parse @@ -oldStart,oldCount +newStart,newCount @@
	_, err := fmt.Sscanf(header, "@@ -%d,%d +%d,%d @@",
		&hunk.OldStart, &hunk.OldCount, &hunk.NewStart, &hunk.NewCount)
	if err != nil {
		// Try without counts (single line hunks)
		_, err2 := fmt.Sscanf(header, "@@ -%d +%d @@",
			&hunk.OldStart, &hunk.NewStart)
		if err2 != nil {
			// Try mixed formats
			_, err3 := fmt.Sscanf(header, "@@ -%d,%d +%d @@",
				&hunk.OldStart, &hunk.OldCount, &hunk.NewStart)
			if err3 != nil {
				_, err4 := fmt.Sscanf(header, "@@ -%d +%d,%d @@",
					&hunk.OldStart, &hunk.NewStart, &hunk.NewCount)
				if err4 != nil {
					return nil, 0, fmt.Errorf("malformed hunk header at line %d: %q", start+1, header)
				}
			} else {
				hunk.NewCount = 1
			}
		} else {
			hunk.OldCount = 1
			hunk.NewCount = 1
		}
	}

	i := start + 1
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "@@ ") {
			break
		}
		if len(line) == 0 {
			// An empty line could be a context line with empty content,
			// but only if we haven't consumed all expected old lines yet.
			oldLinesConsumed := 0
			newLinesConsumed := 0
			for _, dl := range hunk.Lines {
				switch dl.Op {
				case OpContext:
					oldLinesConsumed++
					newLinesConsumed++
				case OpDelete:
					oldLinesConsumed++
				case OpAdd:
					newLinesConsumed++
				}
			}
			if oldLinesConsumed >= hunk.OldCount && newLinesConsumed >= hunk.NewCount {
				break
			}
			hunk.Lines = append(hunk.Lines, DiffLine{Op: OpContext, Content: ""})
			i++
			continue
		}

		op := DiffOp(line[0])
		switch op {
		case OpContext, OpAdd, OpDelete:
			content := ""
			if len(line) > 1 {
				content = line[1:]
			}
			hunk.Lines = append(hunk.Lines, DiffLine{Op: op, Content: content})
		case '\\':
			// "\ No newline at end of file" — skip
		default:
			// Treat as context line (some diffs omit the leading space)
			hunk.Lines = append(hunk.Lines, DiffLine{Op: OpContext, Content: line})
		}
		i++
	}

	return hunk, i, nil
}

// parseDiffGitLine extracts old and new paths from "diff --git a/path b/path".
func parseDiffGitLine(line string) []string {
	prefix := "diff --git "
	if !strings.HasPrefix(line, prefix) {
		return nil
	}
	rest := line[len(prefix):]

	// Handle "a/path b/path" format
	if strings.HasPrefix(rest, "a/") {
		// Find the " b/" separator
		idx := strings.Index(rest, " b/")
		if idx < 0 {
			return nil
		}
		oldPath := rest[2:idx]
		newPath := rest[idx+3:]
		return []string{oldPath, newPath}
	}

	// Fallback: split on space
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return nil
	}
	return parts
}

// Apply applies a PatchAction to the filesystem rooted at cwd.
// It validates baseline content before applying changes.
// If any change fails midway, all previously applied changes are rolled back
// to preserve file integrity.
func Apply(action *PatchAction, cwd string) error {
	backups := make(map[string][]byte)

	for idx, change := range action.Changes {
		// Capture file state before applying the change
		if err := captureBackup(&change, cwd, backups); err != nil {
			// Rollback any changes already applied
			_ = Rollback(action, cwd, backups)
			return fmt.Errorf("change %d (%s %s): %w", idx, change.Mode, changePath(&change), err)
		}

		if err := applyFileChange(&change, cwd); err != nil {
			// Rollback all partial changes
			_ = Rollback(action, cwd, backups)
			return fmt.Errorf("change %d (%s %s): %w", idx, change.Mode, changePath(&change), err)
		}
	}
	return nil
}

func changePath(fc *FileChange) string {
	if fc.NewPath != "" {
		return fc.NewPath
	}
	return fc.OldPath
}

func applyFileChange(fc *FileChange, cwd string) error {
	switch fc.Mode {
	case ModeCreate:
		return applyCreate(fc, cwd)
	case ModeDelete:
		return applyDelete(fc, cwd)
	case ModeRename:
		return applyRename(fc, cwd)
	case ModeModify:
		return applyModify(fc, cwd)
	default:
		return fmt.Errorf("unknown change mode: %s", fc.Mode)
	}
}

func applyCreate(fc *FileChange, cwd string) error {
	target := filepath.Join(cwd, fc.NewPath)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Build content from hunks (all lines should be additions)
	var lines []string
	for _, hunk := range fc.Hunks {
		for _, line := range hunk.Lines {
			if line.Op == OpAdd || line.Op == OpContext {
				lines = append(lines, line.Content)
			}
		}
	}

	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}

	return os.WriteFile(target, []byte(content), 0o644)
}

func applyDelete(fc *FileChange, cwd string) error {
	target := filepath.Join(cwd, fc.OldPath)

	// Validate file exists
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("file to delete does not exist: %w", err)
	}

	return os.Remove(target)
}

func applyRename(fc *FileChange, cwd string) error {
	oldTarget := filepath.Join(cwd, fc.OldPath)
	newTarget := filepath.Join(cwd, fc.NewPath)

	// Validate source exists
	if _, err := os.Stat(oldTarget); err != nil {
		return fmt.Errorf("source file for rename does not exist: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(newTarget), 0o755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	// Rename the file
	if err := os.Rename(oldTarget, newTarget); err != nil {
		return fmt.Errorf("renaming file: %w", err)
	}

	// If there are hunks, apply them to the renamed file
	if len(fc.Hunks) > 0 {
		modFC := &FileChange{
			Mode:    ModeModify,
			OldPath: fc.NewPath,
			NewPath: fc.NewPath,
			Hunks:   fc.Hunks,
		}
		return applyModify(modFC, cwd)
	}

	return nil
}

func applyModify(fc *FileChange, cwd string) error {
	target := filepath.Join(cwd, fc.NewPath)
	if fc.NewPath == "" {
		target = filepath.Join(cwd, fc.OldPath)
	}

	// Read existing content
	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("reading file for modification: %w", err)
	}

	originalLines := splitLines(string(data))

	// Validate baseline content against hunks before applying
	if err := validateBaseline(originalLines, fc.Hunks); err != nil {
		return fmt.Errorf("baseline validation failed: %w", err)
	}

	// Apply hunks in reverse order to preserve line numbers
	result := make([]string, len(originalLines))
	copy(result, originalLines)

	for hi := len(fc.Hunks) - 1; hi >= 0; hi-- {
		hunk := &fc.Hunks[hi]
		result, err = applyHunk(result, hunk)
		if err != nil {
			return fmt.Errorf("applying hunk %d: %w", hi, err)
		}
	}

	// Write back
	output := strings.Join(result, "\n")
	if len(result) > 0 && result[len(result)-1] == "" {
		// File already ends with newline from join
	} else if len(data) > 0 && data[len(data)-1] == '\n' {
		output += "\n"
	}

	return os.WriteFile(target, []byte(output), 0o644)
}

// validateBaseline checks that the context and deletion lines in hunks
// match the actual file content.
func validateBaseline(lines []string, hunks []Hunk) error {
	for hi, hunk := range hunks {
		lineIdx := hunk.OldStart - 1 // Convert 1-based to 0-based
		for _, dl := range hunk.Lines {
			switch dl.Op {
			case OpContext, OpDelete:
				if lineIdx >= len(lines) {
					return fmt.Errorf("hunk %d: expected line %d but file only has %d lines",
						hi, lineIdx+1, len(lines))
				}
				if lines[lineIdx] != dl.Content {
					return fmt.Errorf("hunk %d: line %d mismatch: expected %q, got %q",
						hi, lineIdx+1, dl.Content, lines[lineIdx])
				}
				lineIdx++
			case OpAdd:
				// Addition lines don't consume original lines
			}
		}
	}
	return nil
}

// applyHunk applies a single hunk to the lines slice.
func applyHunk(lines []string, hunk *Hunk) ([]string, error) {
	startIdx := hunk.OldStart - 1 // Convert 1-based to 0-based

	// Build the replacement segment from hunk lines
	var replacement []string
	oldConsumed := 0
	for _, dl := range hunk.Lines {
		switch dl.Op {
		case OpContext:
			replacement = append(replacement, dl.Content)
			oldConsumed++
		case OpAdd:
			replacement = append(replacement, dl.Content)
		case OpDelete:
			oldConsumed++
		}
	}

	// Replace the old segment with the new one
	result := make([]string, 0, len(lines)-oldConsumed+len(replacement))
	result = append(result, lines[:startIdx]...)
	result = append(result, replacement...)
	if startIdx+oldConsumed <= len(lines) {
		result = append(result, lines[startIdx+oldConsumed:]...)
	}

	return result, nil
}

// splitLines splits content into lines, handling the trailing newline correctly.
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	// Remove trailing newline to avoid an extra empty element
	if strings.HasSuffix(content, "\n") {
		content = content[:len(content)-1]
	}
	return strings.Split(content, "\n")
}
