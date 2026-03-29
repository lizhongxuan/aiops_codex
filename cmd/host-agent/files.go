package main

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
)

func handleAgentFileList(runtime *hostAgentRuntime, sender *agentStreamSender, req *agentrpc.FileListRequest) error {
	if req == nil || strings.TrimSpace(req.RequestID) == "" {
		return errors.New("file list requires requestId")
	}
	if runtime != nil && runtime.profile != nil {
		if err := runtime.profile.ensureCapabilityAllowed("fileRead"); err != nil {
			return err
		}
	}

	resolved, err := resolveAgentFilePath(req.Path)
	result := &agentrpc.FileListResult{
		RequestID: safeFileRequestID(req.RequestID),
		Path:      resolved,
	}
	if err != nil {
		result.Message = err.Error()
		return sender.send(&agentrpc.Envelope{
			Kind:           "file/list/result",
			FileListResult: result,
		})
	}

	entries, truncated, listErr := listAgentFiles(resolved, req.Recursive, req.MaxEntries)
	result.Path = resolved
	result.Entries = entries
	result.Truncated = truncated
	if listErr != nil {
		result.Message = listErr.Error()
	}
	return sender.send(&agentrpc.Envelope{
		Kind:           "file/list/result",
		FileListResult: result,
	})
}

func handleAgentFileRead(runtime *hostAgentRuntime, sender *agentStreamSender, req *agentrpc.FileReadRequest) error {
	if req == nil || strings.TrimSpace(req.RequestID) == "" {
		return errors.New("file read requires requestId")
	}
	if runtime != nil && runtime.profile != nil {
		if err := runtime.profile.ensureCapabilityAllowed("fileRead"); err != nil {
			return err
		}
	}

	resolved, err := resolveAgentFilePath(req.Path)
	result := &agentrpc.FileReadResult{
		RequestID: safeFileRequestID(req.RequestID),
		Path:      resolved,
	}
	if err != nil {
		result.Message = err.Error()
		return sender.send(&agentrpc.Envelope{
			Kind:           "file/read/result",
			FileReadResult: result,
		})
	}

	content, truncated, readErr := readAgentFile(resolved, req.MaxBytes)
	result.Path = resolved
	result.Content = content
	result.Truncated = truncated
	if readErr != nil {
		result.Message = readErr.Error()
	}
	return sender.send(&agentrpc.Envelope{
		Kind:           "file/read/result",
		FileReadResult: result,
	})
}

func handleAgentFileSearch(runtime *hostAgentRuntime, sender *agentStreamSender, req *agentrpc.FileSearchRequest) error {
	if req == nil || strings.TrimSpace(req.RequestID) == "" {
		return errors.New("file search requires requestId")
	}
	if strings.TrimSpace(req.Query) == "" {
		return errors.New("file search requires query")
	}
	if runtime != nil && runtime.profile != nil {
		if err := runtime.profile.ensureCapabilityAllowed("fileSearch"); err != nil {
			return err
		}
	}

	resolved, err := resolveAgentFilePath(req.Path)
	result := &agentrpc.FileSearchResult{
		RequestID: safeFileRequestID(req.RequestID),
		Path:      resolved,
		Query:     strings.TrimSpace(req.Query),
	}
	if err != nil {
		result.Message = err.Error()
		return sender.send(&agentrpc.Envelope{
			Kind:             "file/search/result",
			FileSearchResult: result,
		})
	}

	matches, truncated, searchErr := searchAgentFiles(resolved, req.Query, req.MaxMatches)
	result.Path = resolved
	result.Matches = matches
	result.Truncated = truncated
	if searchErr != nil {
		result.Message = searchErr.Error()
	}
	return sender.send(&agentrpc.Envelope{
		Kind:             "file/search/result",
		FileSearchResult: result,
	})
}

func handleAgentFileWrite(runtime *hostAgentRuntime, sender *agentStreamSender, req *agentrpc.FileWriteRequest) error {
	if req == nil || strings.TrimSpace(req.RequestID) == "" {
		return errors.New("file write requires requestId")
	}
	if runtime != nil && runtime.profile != nil {
		if err := runtime.profile.ensureCapabilityAllowed("fileChange"); err != nil {
			return err
		}
	}

	resolved, err := resolveAgentFilePath(req.Path)
	result := &agentrpc.FileWriteResult{
		RequestID: safeFileRequestID(req.RequestID),
		Path:      resolved,
	}
	if err != nil {
		result.Message = err.Error()
		return sender.send(&agentrpc.Envelope{
			Kind:            "file/write/result",
			FileWriteResult: result,
		})
	}
	if runtime != nil && runtime.profile != nil {
		if err := runtime.profile.ensureWritableRoots([]string{resolved}); err != nil {
			result.Message = err.Error()
			return sender.send(&agentrpc.Envelope{
				Kind:            "file/write/result",
				FileWriteResult: result,
			})
		}
	}

	oldContent, newContent, created, writeErr := writeAgentFile(resolved, req.Content, req.WriteMode)
	result.Path = resolved
	result.OldContent = oldContent
	result.NewContent = newContent
	result.Created = created
	result.WriteMode = strings.TrimSpace(req.WriteMode)
	if writeErr != nil {
		result.Message = writeErr.Error()
	}
	return sender.send(&agentrpc.Envelope{
		Kind:            "file/write/result",
		FileWriteResult: result,
	})
}

func resolveAgentFilePath(requested string) (string, error) {
	resolved, err := resolveAgentTerminalCwd(requested)
	if err == nil {
		return resolved, nil
	}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil || home == "" {
		home = "/tmp"
	}
	path := strings.TrimSpace(requested)
	if path == "" || path == "~" {
		path = home
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	return filepath.Abs(filepath.Clean(path))
}

func listAgentFiles(root string, recursive bool, maxEntries int) ([]agentrpc.FileEntry, bool, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return nil, false, errors.New("path is not a directory")
	}

	limit := clampPositive(maxEntries, 200, 1000)
	entries := make([]agentrpc.FileEntry, 0, minInt(limit, 64))
	truncated := false

	appendEntry := func(path string, entry fs.DirEntry) bool {
		if len(entries) >= limit {
			truncated = true
			return false
		}
		info, err := entry.Info()
		size := int64(0)
		if err == nil {
			size = info.Size()
		}
		entries = append(entries, agentrpc.FileEntry{
			Name: entry.Name(),
			Path: path,
			Kind: fileEntryKind(entry),
			Size: size,
		})
		return true
	}

	if !recursive {
		list, err := os.ReadDir(root)
		if err != nil {
			return nil, false, err
		}
		for _, entry := range list {
			if !appendEntry(filepath.Join(root, entry.Name()), entry) {
				break
			}
		}
		return entries, truncated, nil
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == root {
			return nil
		}
		if !appendEntry(path, entry) {
			return fs.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.SkipAll) {
		err = nil
	}
	return entries, truncated, err
}

func readAgentFile(path string, maxBytes int) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, errors.New("path is a directory")
	}

	limit := int64(clampPositive(maxBytes, 64*1024, 512*1024))
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	buf := make([]byte, limit+1)
	n, err := reader.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	data := buf[:n]
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	if looksBinaryContent(data) {
		return "", false, errors.New("binary file preview is not supported")
	}
	return string(data), truncated, nil
}

func searchAgentFiles(root, query string, maxMatches int) ([]agentrpc.FileMatch, bool, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, false, err
	}

	limit := clampPositive(maxMatches, 50, 500)
	query = strings.TrimSpace(query)
	matches := make([]agentrpc.FileMatch, 0, minInt(limit, 32))
	truncated := false

	searchFile := func(path string) error {
		content, _, err := readAgentFile(path, 128*1024)
		if err != nil {
			return nil
		}
		scanner := bufio.NewScanner(strings.NewReader(content))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if !strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
				continue
			}
			if len(matches) >= limit {
				truncated = true
				return fs.SkipAll
			}
			matches = append(matches, agentrpc.FileMatch{
				Path:    path,
				Line:    lineNo,
				Preview: truncatePreview(strings.TrimSpace(line), 180),
			})
		}
		return nil
	}

	if !info.IsDir() {
		err = searchFile(root)
		if errors.Is(err, fs.SkipAll) {
			err = nil
		}
		return matches, truncated, err
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		return searchFile(path)
	})
	if errors.Is(err, fs.SkipAll) {
		err = nil
	}
	return matches, truncated, err
}

func writeAgentFile(path, content, writeMode string) (string, string, bool, error) {
	var oldContent string
	created := false

	if existing, err := os.ReadFile(path); err == nil {
		if looksBinaryContent(existing) {
			return "", "", false, errors.New("binary file editing is not supported")
		}
		oldContent = string(existing)
	} else if errors.Is(err, os.ErrNotExist) {
		created = true
	} else {
		return "", "", false, err
	}

	finalContent := content
	if strings.EqualFold(strings.TrimSpace(writeMode), "append") {
		finalContent = oldContent + content
	}

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return oldContent, finalContent, created, err
	}

	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(parent, ".aiops-write-*")
	if err != nil {
		return oldContent, finalContent, created, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(finalContent); err != nil {
		_ = tmp.Close()
		return oldContent, finalContent, created, err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return oldContent, finalContent, created, err
	}
	if err := tmp.Close(); err != nil {
		return oldContent, finalContent, created, err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return oldContent, finalContent, created, err
	}
	return oldContent, finalContent, created, nil
}

func fileEntryKind(entry fs.DirEntry) string {
	if entry.IsDir() {
		return "dir"
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if entry.Type().IsRegular() {
		return "file"
	}
	return "other"
}

func looksBinaryContent(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func truncatePreview(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func clampPositive(value, fallback, upper int) int {
	if value <= 0 {
		value = fallback
	}
	if upper > 0 && value > upper {
		return upper
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func safeFileRequestID(requestID string) string {
	return strings.TrimSpace(requestID)
}
