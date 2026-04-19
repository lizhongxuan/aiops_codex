package server

// PromptSection represents a named section of the effective prompt.
type PromptSection struct {
	Name      string
	Content   string
	Static    bool
	CacheHint string
}

func defaultPromptSections() []PromptSection {
	return []PromptSection{
		withPromptSectionMeta(staticSystemPromptSection(), true, "static:system"),
		withPromptSectionMeta(developerInstructionsSection(), true, "static:developer"),
		withPromptSectionMeta(intentClarificationSection(), true, "static:intent"),
	}
}

func withPromptSectionMeta(section PromptSection, isStatic bool, cacheHint string) PromptSection {
	section.Static = isStatic
	section.CacheHint = cacheHint
	return section
}
