package agentloop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterListDirTool registers the list_dir tool.
func RegisterListDirTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "list_dir",
		Description: "List directory contents. Accepts a path and optional max_depth (default 1).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to list (relative to working directory).",
				},
				"max_depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum recursion depth (default 1, max 5).",
				},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
		Handler:    handleListDir,
		IsReadOnly: true,
	})
}

func handleListDir(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	dirPath, _ := args["path"].(string)
	if dirPath == "" {
		dirPath = "."
	}

	maxDepth := 1
	if d, ok := args["max_depth"].(float64); ok && d > 0 {
		maxDepth = int(d)
	}
	if maxDepth > 5 {
		maxDepth = 5
	}

	// Resolve relative to session cwd.
	cwd := session.Cwd()
	if cwd == "" {
		cwd = "."
	}
	if !filepath.IsAbs(dirPath) {
		dirPath = filepath.Join(cwd, dirPath)
	}

	var sb strings.Builder
	err := walkDir(&sb, dirPath, "", 0, maxDepth)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}
	return sb.String(), nil
}

func walkDir(sb *strings.Builder, root, prefix string, depth, maxDepth int) error {
	if depth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files at depth 0 for cleaner output.
		if strings.HasPrefix(name, ".") && depth == 0 {
			continue
		}

		if entry.IsDir() {
			sb.WriteString(fmt.Sprintf("%s%s/\n", prefix, name))
			if depth+1 < maxDepth {
				_ = walkDir(sb, filepath.Join(root, name), prefix+"  ", depth+1, maxDepth)
			}
		} else {
			info, _ := entry.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			sb.WriteString(fmt.Sprintf("%s%s (%d bytes)\n", prefix, name, size))
		}
	}
	return nil
}
