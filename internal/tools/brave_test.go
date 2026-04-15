package tools

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// Property 16: Brave search result parsing — every braveWebResult in the API
// response is correctly mapped to a WebSearchResult with matching fields.
func TestProperty_BraveSearchResultParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 20).Draw(t, "numResults")
		braveResults := make([]braveWebResult, n)
		for i := 0; i < n; i++ {
			braveResults[i] = braveWebResult{
				Title:       rapid.String().Draw(t, "title"),
				URL:         rapid.String().Draw(t, "url"),
				Description: rapid.String().Draw(t, "description"),
			}
		}

		apiResp := braveSearchResponse{}
		apiResp.Web.Results = braveResults
		data, err := json.Marshal(apiResp)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		results, err := parseBraveResponse(data)
		if err != nil {
			t.Fatalf("parseBraveResponse: %v", err)
		}

		expected := n
		if expected > braveMaxResults {
			expected = braveMaxResults
		}
		if len(results) != expected {
			t.Fatalf("got %d results, want %d", len(results), expected)
		}

		for i, r := range results {
			if r.Title != braveResults[i].Title {
				t.Fatalf("result[%d].Title = %q, want %q", i, r.Title, braveResults[i].Title)
			}
			if r.URL != braveResults[i].URL {
				t.Fatalf("result[%d].URL = %q, want %q", i, r.URL, braveResults[i].URL)
			}
			if r.Snippet != braveResults[i].Description {
				t.Fatalf("result[%d].Snippet = %q, want %q", i, r.Snippet, braveResults[i].Description)
			}
		}
	})
}

// Property 17: Brave search result limit — parseBraveResponse never returns
// more than braveMaxResults (10) entries regardless of input size.
func TestProperty_BraveSearchResultLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 50).Draw(t, "numResults")
		braveResults := make([]braveWebResult, n)
		for i := 0; i < n; i++ {
			braveResults[i] = braveWebResult{
				Title:       rapid.String().Draw(t, "title"),
				URL:         rapid.String().Draw(t, "url"),
				Description: rapid.String().Draw(t, "description"),
			}
		}

		apiResp := braveSearchResponse{}
		apiResp.Web.Results = braveResults
		data, err := json.Marshal(apiResp)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		results, err := parseBraveResponse(data)
		if err != nil {
			t.Fatalf("parseBraveResponse: %v", err)
		}

		if len(results) > braveMaxResults {
			t.Fatalf("got %d results, exceeds max %d", len(results), braveMaxResults)
		}
	})
}
