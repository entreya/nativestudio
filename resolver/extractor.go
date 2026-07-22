package resolver

import (
	"regexp"
	"strings"
)

// PromptRefs holds all extracted references from a user's prompt text.
type PromptRefs struct {
	// HasThis is true when the prompt contains a "this <kind>" pattern.
	HasThis bool
	// ThisKind is the kind word after "this" (e.g. "method", "class").
	ThisKind string
	// SameKind is the noun after "same" (e.g. "same pattern").
	SameKind string
	// ExplicitFiles are file paths or names explicitly mentioned in the prompt.
	ExplicitFiles []string
	// ExplicitNames are PascalCase identifiers found in the prompt.
	ExplicitNames []string
	// HasSame is true when the prompt says "same X".
	HasSame bool
	// HasPrevious is true when the prompt references "previous/last change/edit".
	HasPrevious bool
}

var (
	// thisKindRe matches "this method", "this class", "this file", etc.
	thisKindRe = regexp.MustCompile(`(?i)\bthis\s+(method|function|class|file|service|controller|handler|interface|trait)\b`)

	// sameRe matches "same X" where X is a word.
	sameRe = regexp.MustCompile(`(?i)\bsame\s+(\w+)\b`)

	// fileRefRe matches file paths with known extensions.
	fileRefRe = regexp.MustCompile(`\b[\w./\-]+\.(php|go|ts|js|py|md|jsx|tsx|json|yaml|yml)\b`)

	// pascalCaseRe matches PascalCase identifiers (at least two chars, starts uppercase).
	pascalCaseRe = regexp.MustCompile(`\b[A-Z][a-zA-Z0-9]{1,}\b`)

	// previousRe matches references to previous/last change or edit.
	previousRe = regexp.MustCompile(`(?i)\b(previous\s+change|last\s+change|last\s+edit)\b`)
)

// ExtractRefs parses a user prompt and returns all structural references found.
func ExtractRefs(prompt string) PromptRefs {
	var refs PromptRefs

	// Check for "this <kind>"
	if m := thisKindRe.FindStringSubmatch(prompt); m != nil {
		refs.HasThis = true
		refs.ThisKind = strings.ToLower(m[1])
	}

	// Check for "same <word>"
	if m := sameRe.FindStringSubmatch(prompt); m != nil {
		refs.HasSame = true
		refs.SameKind = strings.ToLower(m[1])
	}

	// Check for previous/last change references
	if previousRe.MatchString(prompt) {
		refs.HasPrevious = true
	}

	// Extract explicit file references
	for _, m := range fileRefRe.FindAllString(prompt, -1) {
		refs.ExplicitFiles = append(refs.ExplicitFiles, m)
	}

	// Extract PascalCase identifiers — filter common English words to reduce noise
	commonWords := map[string]bool{
		"I": true, "In": true, "If": true, "It": true, "Is": true,
		"The": true, "This": true, "That": true, "Then": true,
		"When": true, "What": true, "How": true, "Who": true,
		"Can": true, "Please": true, "Fix": true, "Add": true,
		"Make": true, "Use": true, "Get": true, "Set": true,
		"Run": true, "Try": true, "For": true,
	}
	for _, m := range pascalCaseRe.FindAllString(prompt, -1) {
		if !commonWords[m] {
			refs.ExplicitNames = append(refs.ExplicitNames, m)
		}
	}

	return refs
}
