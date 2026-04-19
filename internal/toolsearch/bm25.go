// Package toolsearch provides BM25-based tool search and ranking.
package toolsearch

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// ToolDoc represents a document in the BM25 index, typically derived from a tool definition.
type ToolDoc struct {
	Name        string
	Description string
	// Tags are optional keywords for boosting relevance.
	Tags []string
}

// SearchResult holds a ranked search result.
type SearchResult struct {
	Name  string
	Score float64
}

// Index is a BM25 search index over tool documents.
type Index struct {
	docs     []ToolDoc
	avgDL    float64
	docFreqs map[string]int // term -> number of docs containing term
	termFreq []map[string]int // per-doc term frequencies
	docLens  []int
	k1       float64
	b        float64
}

// NewIndex builds a BM25 index from the given tool documents.
func NewIndex(tools []ToolDoc) *Index {
	idx := &Index{
		docs:     tools,
		docFreqs: make(map[string]int),
		termFreq: make([]map[string]int, len(tools)),
		docLens:  make([]int, len(tools)),
		k1:       1.5,
		b:        0.75,
	}

	totalLen := 0
	for i, doc := range tools {
		tokens := tokenize(doc.Name + " " + doc.Description + " " + strings.Join(doc.Tags, " "))
		idx.docLens[i] = len(tokens)
		totalLen += len(tokens)

		tf := make(map[string]int)
		seen := make(map[string]bool)
		for _, t := range tokens {
			tf[t]++
			if !seen[t] {
				seen[t] = true
				idx.docFreqs[t]++
			}
		}
		idx.termFreq[i] = tf
	}

	if len(tools) > 0 {
		idx.avgDL = float64(totalLen) / float64(len(tools))
	}
	return idx
}

// Search returns the top-K results for the given query, ranked by BM25 score.
// Results with score <= 0 are excluded.
func (idx *Index) Search(query string, topK int) []SearchResult {
	if len(idx.docs) == 0 || topK <= 0 {
		return nil
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	n := float64(len(idx.docs))
	scores := make([]SearchResult, 0, len(idx.docs))

	for i, doc := range idx.docs {
		score := 0.0
		dl := float64(idx.docLens[i])

		for _, term := range queryTerms {
			df, ok := idx.docFreqs[term]
			if !ok {
				continue
			}
			tf := float64(idx.termFreq[i][term])
			// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
			idf := math.Log((n-float64(df)+0.5)/(float64(df)+0.5) + 1.0)
			// TF normalization
			tfNorm := (tf * (idx.k1 + 1)) / (tf + idx.k1*(1-idx.b+idx.b*(dl/idx.avgDL)))
			score += idf * tfNorm
		}

		if score > 0 {
			scores = append(scores, SearchResult{Name: doc.Name, Score: score})
		}
		_ = doc // suppress unused warning
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	if topK > len(scores) {
		topK = len(scores)
	}
	return scores[:topK]
}

// tokenize splits text into lowercase tokens, splitting on non-alphanumeric characters.
func tokenize(text string) []string {
	var tokens []string
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) > 1 { // skip single-char tokens
			tokens = append(tokens, word)
		}
	}
	return tokens
}
