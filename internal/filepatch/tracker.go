package filepatch

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"sort"
	"strings"
)

// FileBaseline holds the captured state of a file at the start of a turn.
type FileBaseline struct {
	Content []byte
	SHA1    string // git blob OID
}

// FileDiff represents a git-compatible unified diff for a single file.
type FileDiff struct {
	Path    string
	OldOID  string
	NewOID  string
	Patch   string
}

// TurnDiffTracker captures file baselines at turn start and generates
// unified diffs for all modified files at turn end.
type TurnDiffTracker struct {
	baselines map[string]FileBaseline
}

// NewTurnDiffTracker creates a new TurnDiffTracker.
func NewTurnDiffTracker() *TurnDiffTracker {
	return &TurnDiffTracker{
		baselines: make(map[string]FileBaseline),
	}
}

// RecordBaseline captures the current file state for the given path.
// If the file does not exist, it records an empty baseline (new file scenario).
func (t *TurnDiffTracker) RecordBaseline(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet — record empty baseline
			t.baselines[path] = FileBaseline{
				Content: nil,
				SHA1:    blobOID(nil),
			}
			return nil
		}
		return fmt.Errorf("recording baseline for %s: %w", path, err)
	}
	t.baselines[path] = FileBaseline{
		Content: data,
		SHA1:    blobOID(data),
	}
	return nil
}

// GenerateDiff produces git-compatible unified diffs for all files that
// have been modified since their baseline was recorded.
func (t *TurnDiffTracker) GenerateDiff() ([]FileDiff, error) {
	var diffs []FileDiff

	// Sort paths for deterministic output
	paths := make([]string, 0, len(t.baselines))
	for p := range t.baselines {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		baseline := t.baselines[path]

		currentData, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				// File was deleted — generate a deletion diff
				if len(baseline.Content) == 0 {
					continue // was empty and now gone, no diff
				}
				diff := generateUnifiedDiff(path, baseline.Content, nil)
				diffs = append(diffs, FileDiff{
					Path:   path,
					OldOID: baseline.SHA1,
					NewOID: blobOID(nil),
					Patch:  diff,
				})
				continue
			}
			return nil, fmt.Errorf("reading current state of %s: %w", path, err)
		}

		// Skip unchanged files
		if bytes.Equal(baseline.Content, currentData) {
			continue
		}

		newOID := blobOID(currentData)
		diff := generateUnifiedDiff(path, baseline.Content, currentData)
		diffs = append(diffs, FileDiff{
			Path:   path,
			OldOID: baseline.SHA1,
			NewOID: newOID,
			Patch:  diff,
		})
	}

	return diffs, nil
}

// blobOID computes the git blob SHA1 OID for the given content.
// Git blob format: "blob <size>\x00<content>"
func blobOID(data []byte) string {
	if data == nil {
		data = []byte{}
	}
	header := fmt.Sprintf("blob %d\x00", len(data))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// generateUnifiedDiff produces a git-style unified diff between old and new content.
func generateUnifiedDiff(path string, oldContent, newContent []byte) string {
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)

	// Determine mode
	var oldPath, newPath string
	if oldContent == nil {
		oldPath = "/dev/null"
		newPath = "b/" + path
	} else if newContent == nil {
		oldPath = "a/" + path
		newPath = "/dev/null"
	} else {
		oldPath = "a/" + path
		newPath = "b/" + path
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))

	if oldContent == nil {
		buf.WriteString("new file mode 100644\n")
	} else if newContent == nil {
		buf.WriteString("deleted file mode 100644\n")
	}

	buf.WriteString(fmt.Sprintf("--- %s\n", oldPath))
	buf.WriteString(fmt.Sprintf("+++ %s\n", newPath))

	// Generate hunks using a simple diff algorithm
	hunks := computeHunks(oldLines, newLines)
	for _, hunk := range hunks {
		buf.WriteString(hunk)
	}

	return buf.String()
}

// splitContentLines splits content into lines for diffing.
func splitContentLines(data []byte) []string {
	if data == nil || len(data) == 0 {
		return []string{}
	}
	s := string(data)
	if strings.HasSuffix(s, "\n") {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n")
}

// computeHunks generates unified diff hunks from old and new line slices.
// Uses a simple LCS-based approach with context lines.
func computeHunks(oldLines, newLines []string) []string {
	const contextLines = 3

	// Compute edit script using LCS
	edits := computeEdits(oldLines, newLines)

	if len(edits) == 0 {
		return nil
	}

	// Group edits into hunks with context
	var hunks []string
	var currentHunk strings.Builder
	hunkOldStart, hunkNewStart := 0, 0
	hunkOldCount, hunkNewCount := 0, 0
	inHunk := false
	lastChangeIdx := -1

	for i, edit := range edits {
		isChange := edit.op != opEqual

		if isChange {
			if !inHunk {
				// Start a new hunk with leading context
				inHunk = true
				contextStart := i - contextLines
				if contextStart < 0 {
					contextStart = 0
				}
				hunkOldStart = edits[contextStart].oldIdx + 1
				hunkNewStart = edits[contextStart].newIdx + 1
				hunkOldCount = 0
				hunkNewCount = 0
				currentHunk.Reset()

				// Add leading context
				for j := contextStart; j < i; j++ {
					if edits[j].op == opEqual {
						currentHunk.WriteString(" " + edits[j].line + "\n")
						hunkOldCount++
						hunkNewCount++
					}
				}
			}
			lastChangeIdx = i
		}

		if inHunk {
			if isChange {
				switch edit.op {
				case opDelete:
					currentHunk.WriteString("-" + edit.line + "\n")
					hunkOldCount++
				case opInsert:
					currentHunk.WriteString("+" + edit.line + "\n")
					hunkNewCount++
				}
			} else {
				// Context after a change
				distFromLastChange := i - lastChangeIdx
				if distFromLastChange > contextLines {
					// Close current hunk
					hunks = append(hunks, fmt.Sprintf("@@ -%d,%d +%d,%d @@\n%s",
						hunkOldStart, hunkOldCount, hunkNewStart, hunkNewCount, currentHunk.String()))
					inHunk = false
				} else {
					currentHunk.WriteString(" " + edit.line + "\n")
					hunkOldCount++
					hunkNewCount++
				}
			}
		}
	}

	// Close final hunk
	if inHunk {
		hunks = append(hunks, fmt.Sprintf("@@ -%d,%d +%d,%d @@\n%s",
			hunkOldStart, hunkOldCount, hunkNewStart, hunkNewCount, currentHunk.String()))
	}

	return hunks
}

type editOp int

const (
	opEqual  editOp = iota
	opDelete
	opInsert
)

type edit struct {
	op     editOp
	line   string
	oldIdx int // 0-based line index in old file
	newIdx int // 0-based line index in new file
}

// computeEdits produces a sequence of edit operations using Myers' diff algorithm (simplified).
func computeEdits(oldLines, newLines []string) []edit {
	// Compute LCS table
	m, n := len(oldLines), len(newLines)
	// Use DP for LCS
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to produce edit sequence
	var edits []edit
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			edits = append(edits, edit{op: opEqual, line: oldLines[i-1], oldIdx: i - 1, newIdx: j - 1})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, edit{op: opInsert, line: newLines[j-1], oldIdx: i, newIdx: j - 1})
			j--
		} else {
			edits = append(edits, edit{op: opDelete, line: oldLines[i-1], oldIdx: i - 1, newIdx: j})
			i--
		}
	}

	// Reverse to get forward order
	for left, right := 0, len(edits)-1; left < right; left, right = left+1, right-1 {
		edits[left], edits[right] = edits[right], edits[left]
	}

	return edits
}
