package guardian

import (
	"sync"
	"time"
)

// ApprovalDecision represents a cached approval decision.
type ApprovalDecision struct {
	Outcome   AssessmentOutcome `json:"outcome"`
	Rationale string            `json:"rationale"`
	CachedAt  time.Time         `json:"cached_at"`
}

// ApprovalCache stores approval decisions keyed by operation pattern,
// scoped to a single session. It provides fast lookup to avoid redundant
// guardian reviews for previously approved patterns.
type ApprovalCache struct {
	mu       sync.RWMutex
	patterns map[string]ApprovalDecision
}

// NewApprovalCache creates a new empty ApprovalCache.
func NewApprovalCache() *ApprovalCache {
	return &ApprovalCache{
		patterns: make(map[string]ApprovalDecision),
	}
}

// Check looks up a cached approval decision for the given pattern.
// Returns the decision and true if found, or zero value and false if not cached.
func (c *ApprovalCache) Check(pattern string) (ApprovalDecision, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	decision, ok := c.patterns[pattern]
	return decision, ok
}

// Store caches an approval decision for the given pattern.
func (c *ApprovalCache) Store(pattern string, decision ApprovalDecision) {
	c.mu.Lock()
	defer c.mu.Unlock()
	decision.CachedAt = time.Now()
	c.patterns[pattern] = decision
}

// Clear removes all cached approvals. Called on session end.
func (c *ApprovalCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.patterns = make(map[string]ApprovalDecision)
}

// Size returns the number of cached entries.
func (c *ApprovalCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.patterns)
}

// CheckBeforeReview checks the cache before initiating a guardian review.
// Returns the cached assessment if available, or nil if a review is needed.
func CheckBeforeReview(cache *ApprovalCache, request GuardianApprovalRequest) *GuardianAssessment {
	if cache == nil {
		return nil
	}

	pattern := buildCachePattern(request)
	decision, ok := cache.Check(pattern)
	if !ok {
		return nil
	}

	return &GuardianAssessment{
		RiskLevel:         RiskLow,
		UserAuthorization: AuthImplied,
		Outcome:           decision.Outcome,
		Rationale:         "cached: " + decision.Rationale,
	}
}

// CacheDecision stores a guardian assessment result in the cache.
func CacheDecision(cache *ApprovalCache, request GuardianApprovalRequest, assessment *GuardianAssessment) {
	if cache == nil || assessment == nil {
		return
	}

	pattern := buildCachePattern(request)
	cache.Store(pattern, ApprovalDecision{
		Outcome:   assessment.Outcome,
		Rationale: assessment.Rationale,
	})
}

// buildCachePattern creates a cache key from the approval request.
func buildCachePattern(request GuardianApprovalRequest) string {
	return request.ToolName + ":" + request.Arguments
}
