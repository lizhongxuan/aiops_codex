package agentloop

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterViewImageTool registers the view_image tool.
func RegisterViewImageTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "view_image",
		Description: "Load an image file, validate dimensions (max 2048x2048), and return as a base64 data URI.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the image file.",
				},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
		Handler:    handleViewImage,
		IsReadOnly: true,
	})
}

// maxImageSize is the maximum allowed image file size (10MB).
const maxImageSize = 10 * 1024 * 1024

func handleViewImage(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	imgPath, _ := args["path"].(string)
	if imgPath == "" {
		return "", fmt.Errorf("view_image requires a non-empty 'path' argument")
	}

	cwd := session.Cwd()
	if cwd == "" {
		cwd = "."
	}
	if !filepath.IsAbs(imgPath) {
		imgPath = filepath.Join(cwd, imgPath)
	}

	info, err := os.Stat(imgPath)
	if err != nil {
		return "", fmt.Errorf("view_image: %w", err)
	}
	if info.Size() > maxImageSize {
		return "", fmt.Errorf("view_image: file too large (%d bytes, max %d)", info.Size(), maxImageSize)
	}

	data, err := os.ReadFile(imgPath)
	if err != nil {
		return "", fmt.Errorf("view_image: %w", err)
	}

	// Detect MIME type.
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("view_image: file is not a recognized image format (detected: %s)", mimeType)
	}

	// Encode as base64 data URI.
	encoded := base64.StdEncoding.EncodeToString(data)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	return dataURI, nil
}
