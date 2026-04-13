package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	rawMemoriesFile    = "raw_memories.json"
	consolidatedFile   = "consolidated.json"
)

// Storage handles persistence of memory artifacts to a configurable directory.
type Storage struct {
	rootDir string
}

// NewStorage creates a new Storage instance rooted at the given directory.
func NewStorage(rootDir string) *Storage {
	return &Storage{rootDir: rootDir}
}

// ensureDir creates the storage directory if it doesn't exist.
func (s *Storage) ensureDir() error {
	return os.MkdirAll(s.rootDir, 0o755)
}

// SaveRawMemories persists a slice of raw memories to disk.
func (s *Storage) SaveRawMemories(memories []RawMemory) error {
	if err := s.ensureDir(); err != nil {
		return fmt.Errorf("memory/storage: create dir: %w", err)
	}
	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return fmt.Errorf("memory/storage: marshal raw memories: %w", err)
	}
	path := filepath.Join(s.rootDir, rawMemoriesFile)
	return os.WriteFile(path, data, 0o644)
}

// LoadRawMemories loads previously saved raw memories from disk.
func (s *Storage) LoadRawMemories() ([]RawMemory, error) {
	path := filepath.Join(s.rootDir, rawMemoriesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory/storage: read raw memories: %w", err)
	}
	var memories []RawMemory
	if err := json.Unmarshal(data, &memories); err != nil {
		return nil, fmt.Errorf("memory/storage: unmarshal raw memories: %w", err)
	}
	return memories, nil
}

// SaveConsolidated persists a consolidated memory summary to disk.
func (s *Storage) SaveConsolidated(memory *ConsolidatedMemory) error {
	if err := s.ensureDir(); err != nil {
		return fmt.Errorf("memory/storage: create dir: %w", err)
	}
	data, err := json.MarshalIndent(memory, "", "  ")
	if err != nil {
		return fmt.Errorf("memory/storage: marshal consolidated: %w", err)
	}
	path := filepath.Join(s.rootDir, consolidatedFile)
	return os.WriteFile(path, data, 0o644)
}

// LoadConsolidated loads the consolidated memory from disk.
func (s *Storage) LoadConsolidated() (*ConsolidatedMemory, error) {
	path := filepath.Join(s.rootDir, consolidatedFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory/storage: read consolidated: %w", err)
	}
	var mem ConsolidatedMemory
	if err := json.Unmarshal(data, &mem); err != nil {
		return nil, fmt.Errorf("memory/storage: unmarshal consolidated: %w", err)
	}
	return &mem, nil
}

// Clear removes all memory artifacts for a fresh start.
func (s *Storage) Clear() error {
	return os.RemoveAll(s.rootDir)
}
