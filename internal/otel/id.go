package otel

import (
	"crypto/rand"
	"encoding/hex"
)

// generateID generates a random hex ID for trace/span identification.
func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
