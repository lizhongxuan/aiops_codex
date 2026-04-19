package agentloop

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// mentionPattern matches @file-path and @agent-name references.
// Supports: @path/to/file.ext, @agent-name
var mentionPattern = regexp.MustCompile(`@([\w./_-]+[\w./])`)

// MentionType classifies a resolved mention.
type MentionType int

const (
	MentionFile  MentionType = iota // @file-path reference
	MentionAgent                    // @agent-name reference
)

// ResolvedMention represents a parsed and resolved mention.
type ResolvedMention struct {
	Type    MentionType
	Raw     string // Original @reference text
	Name    string // Resolved name (file path or agent name)
	Content string // Injected content (file content or routing info)
}

// ResolveMentions parses @file-path and @agent-name references in user input.
// File references inject file content into context. Agent references route messages.
func ResolveMentions(input string, session *Session) (string, error) {
	if session == nil {
		return input, nil
	}

	matches := mentionPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, nil
	}

	var result strings.Builder
	lastIdx := 0
	var injections []string

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		nameStart, nameEnd := match[2], match[3]
		name := input[nameStart:nameEnd]

		result.WriteString(input[lastIdx:fullStart])

		resolved, err := resolveMention(name, session)
		if err != nil {
			// Keep original text if resolution fails
			result.WriteString(input[fullStart:fullEnd])
		} else {
			result.WriteString(input[fullStart:fullEnd])
			if resolved.Content != "" {
				injections = append(injections, resolved.Content)
			}
		}
		lastIdx = fullEnd
	}
	result.WriteString(input[lastIdx:])

	// Append injected content
	output := result.String()
	if len(injections) > 0 {
		output += "\n\n" + strings.Join(injections, "\n\n")
	}

	return output, nil
}

// resolveMention resolves a single mention reference.
func resolveMention(name string, session *Session) (*ResolvedMention, error) {
	cwd := session.Cwd()

	// Try as file path first
	filePath := name
	if !filepath.IsAbs(filePath) && cwd != "" {
		filePath = filepath.Join(cwd, filePath)
	}

	info, err := os.Stat(filePath)
	if err == nil && !info.IsDir() {
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("read file %s: %w", name, readErr)
		}

		// Truncate large files
		text := string(content)
		const maxFileContent = 32000
		if len(text) > maxFileContent {
			text = text[:maxFileContent] + "\n... [file truncated]"
		}

		return &ResolvedMention{
			Type:    MentionFile,
			Raw:     "@" + name,
			Name:    name,
			Content: fmt.Sprintf("--- Content of %s ---\n%s", name, text),
		}, nil
	}

	// Try as agent name
	return &ResolvedMention{
		Type:    MentionAgent,
		Raw:     "@" + name,
		Name:    name,
		Content: fmt.Sprintf("[Message routed to agent: %s]", name),
	}, nil
}
