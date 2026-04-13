package memory

// Citation tracks a reference linking a consolidated memory back to its source rollout.
type Citation struct {
	MemoryID      string `json:"memory_id"`
	SourceRollout string `json:"source_rollout"`
	Excerpt       string `json:"excerpt"`
}

// CitationTracker manages citation references for consolidated memories.
type CitationTracker struct {
	citations []Citation
}

// NewCitationTracker creates a new empty CitationTracker.
func NewCitationTracker() *CitationTracker {
	return &CitationTracker{}
}

// Add records a new citation reference.
func (ct *CitationTracker) Add(c Citation) {
	ct.citations = append(ct.citations, c)
}

// AddFromRawMemory creates and records a citation from a RawMemory.
func (ct *CitationTracker) AddFromRawMemory(rm RawMemory) {
	ct.citations = append(ct.citations, Citation{
		MemoryID:      rm.ID,
		SourceRollout: rm.SourceRollout,
		Excerpt:       truncate(rm.Summary, 100),
	})
}

// All returns all tracked citations.
func (ct *CitationTracker) All() []Citation {
	return ct.citations
}

// ForRollout returns citations matching a specific source rollout.
func (ct *CitationTracker) ForRollout(rolloutID string) []Citation {
	var result []Citation
	for _, c := range ct.citations {
		if c.SourceRollout == rolloutID {
			result = append(result, c)
		}
	}
	return result
}

// Clear removes all tracked citations.
func (ct *CitationTracker) Clear() {
	ct.citations = nil
}
