package server

import "testing"

func TestDefaultPromptSectionsExposeStableMetadata(t *testing.T) {
	sections := defaultPromptSections()
	if len(sections) != 3 {
		t.Fatalf("expected 3 default sections, got %d", len(sections))
	}

	wantNames := []string{"System", "DeveloperInstructions", "IntentClarification"}
	for i, want := range wantNames {
		if sections[i].Name != want {
			t.Fatalf("expected section %d to be %q, got %q", i, want, sections[i].Name)
		}
		if !sections[i].Static {
			t.Fatalf("expected section %q to be static", want)
		}
		if sections[i].CacheHint == "" {
			t.Fatalf("expected section %q to carry cache hint metadata", want)
		}
		if sections[i].Content == "" {
			t.Fatalf("expected section %q content to be non-empty", want)
		}
	}
}

func TestDefaultPromptSectionsReturnFreshCopies(t *testing.T) {
	sections := defaultPromptSections()
	sections[0].Content = "mutated"

	again := defaultPromptSections()
	if again[0].Content == "mutated" {
		t.Fatal("defaultPromptSections should return fresh copies")
	}
}
