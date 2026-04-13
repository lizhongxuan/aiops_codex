package agentloop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultProjectDocNames lists common project documentation files to discover.
var DefaultProjectDocNames = []string{
	"README.md",
	"README",
	"CONTRIBUTING.md",
	"CONTRIBUTING",
	"ARCHITECTURE.md",
	"DESIGN.md",
	"CHANGELOG.md",
	"CODE_OF_CONDUCT.md",
	"SECURITY.md",
	"docs/README.md",
}

// ProjectDocInjector discovers and injects project documents into session context.
type ProjectDocInjector struct {
	// Paths specifies document filenames to look for (relative to project root).
	Paths []string
	// TokenBudget is the maximum number of tokens to inject (approximate: 4 chars per token).
	TokenBudget int
}

// NewProjectDocInjector creates a ProjectDocInjector with default settings.
func NewProjectDocInjector() *ProjectDocInjector {
	return &ProjectDocInjector{
		Paths:       DefaultProjectDocNames,
		TokenBudget: 4000, // ~16KB of text
	}
}

// Discover finds project documents from configured paths relative to projectRoot.
func (p *ProjectDocInjector) Discover(projectRoot string) []string {
	var found []string
	seen := make(map[string]bool)

	for _, name := range p.Paths {
		fullPath := filepath.Join(projectRoot, name)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			continue
		}
		absPath, _ := filepath.Abs(fullPath)
		if seen[absPath] {
			continue
		}
		seen[absPath] = true
		found = append(found, fullPath)
	}

	return found
}

// Inject adds discovered project documents to the session context within the token budget.
func (p *ProjectDocInjector) Inject(session *Session) error {
	if session == nil {
		return fmt.Errorf("project doc injector: nil session")
	}

	projectRoot := session.Cwd()
	if projectRoot == "" {
		return nil
	}

	docs := p.Discover(projectRoot)
	if len(docs) == 0 {
		return nil
	}

	var sections []string
	remainingBudget := p.TokenBudget
	charsPerToken := 4

	for _, docPath := range docs {
		if remainingBudget <= 0 {
			break
		}

		content, err := os.ReadFile(docPath)
		if err != nil {
			continue
		}

		text := string(content)
		maxChars := remainingBudget * charsPerToken
		if len(text) > maxChars {
			text = text[:maxChars] + "\n... [truncated due to token budget]"
		}

		relPath, _ := filepath.Rel(projectRoot, docPath)
		if relPath == "" {
			relPath = filepath.Base(docPath)
		}

		section := fmt.Sprintf("--- %s ---\n%s", relPath, text)
		sections = append(sections, section)

		tokensUsed := len(text) / charsPerToken
		remainingBudget -= tokensUsed
	}

	if len(sections) == 0 {
		return nil
	}

	injected := "## Project Documentation\n\n" + strings.Join(sections, "\n\n")

	// Add to context manager as a system message
	ctxMgr := session.ContextManager()
	if ctxMgr != nil {
		ctxMgr.AppendSystem(injected)
	}

	return nil
}
