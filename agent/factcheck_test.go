package agent

import (
	"strings"
	"testing"
)

func TestScoreFactCheckCountsIndependentDomainsNotResults(t *testing.T) {
	// Ten hits, but every one from the same outlet. That is one source, and
	// treating it as ten would be exactly the false-corroboration failure
	// cross-checking exists to prevent.
	check := factCheck{Query: "test"}
	for i := 0; i < 10; i++ {
		check.Results = append(check.Results, sourcedResult{
			EngineID: "bing", Category: "web", Domain: "example.com", Independent: true,
		})
	}
	scoreFactCheck(&check)

	if check.Confidence != "low" {
		t.Fatalf("expected low confidence for a single repeated domain, got %q (%s)", check.Confidence, check.Reason)
	}
	if len(check.IndependentDomains) != 1 {
		t.Fatalf("expected 1 independent domain, got %v", check.IndependentDomains)
	}
}

func TestScoreFactCheckIgnoresDerivedSources(t *testing.T) {
	// DuckDuckGo's Instant Answer abstracts are largely Wikipedia text
	// (verified live: AbstractSource came back as "Wikipedia"). Counting both
	// as agreeing sources would manufacture corroboration out of one source.
	check := factCheck{Query: "test", Results: []sourcedResult{
		{EngineID: "wikipedia", Category: "reference", Domain: "en.wikipedia.org", Independent: true},
		{EngineID: "duckduckgo", Category: "reference", Domain: "duckduckgo.com", Independent: false},
	}}
	scoreFactCheck(&check)

	if len(check.IndependentDomains) != 1 {
		t.Fatalf("expected the derived source to be discounted, got %v", check.IndependentDomains)
	}
	if check.Confidence != "low" {
		t.Fatalf("expected low confidence with only one genuinely independent source, got %q", check.Confidence)
	}
}

func TestScoreFactCheckConfidenceLadder(t *testing.T) {
	cases := []struct {
		name     string
		results  []sourcedResult
		expected string
	}{
		{
			name:     "no usable sources",
			results:  nil,
			expected: "low",
		},
		{
			name: "two sources of the same kind may share one wire story",
			results: []sourcedResult{
				{Category: "news", Domain: "a.com", Independent: true},
				{Category: "news", Domain: "b.com", Independent: true},
			},
			expected: "medium",
		},
		{
			name: "two sources of different kinds",
			results: []sourcedResult{
				{Category: "news", Domain: "a.com", Independent: true},
				{Category: "reference", Domain: "b.org", Independent: true},
			},
			expected: "medium",
		},
		{
			name: "three independent domains",
			results: []sourcedResult{
				{Category: "news", Domain: "a.com", Independent: true},
				{Category: "web", Domain: "b.com", Independent: true},
				{Category: "reference", Domain: "c.org", Independent: true},
			},
			expected: "high",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			check := factCheck{Query: "q", Results: testCase.results}
			scoreFactCheck(&check)
			if check.Confidence != testCase.expected {
				t.Fatalf("expected %q, got %q (%s)", testCase.expected, check.Confidence, check.Reason)
			}
		})
	}
}

// TestParseRSSEngineUnwrapsAggregatorPublisher covers the Google News case
// verified live: every link points at news.google.com, but the feed carries
// the real publisher in <source url>. Without unwrapping that, stories from
// many outlets would collapse into a single apparent source.
func TestParseRSSEngineUnwrapsAggregatorPublisher(t *testing.T) {
	feed := `<?xml version="1.0"?><rss><channel>
	  <item>
	    <title>PHP 8.5 released with new features</title>
	    <link>https://news.google.com/rss/articles/ABC123</link>
	    <description>Coverage of the PHP release</description>
	    <source url="https://thehackernews.com">The Hacker News</source>
	  </item>
	  <item>
	    <title>PHP 8.5 release notes and upgrade guide</title>
	    <link>https://news.google.com/rss/articles/DEF456</link>
	    <description>Details of the PHP release</description>
	    <source url="https://www.hostinger.com">Hostinger</source>
	  </item>
	</channel></rss>`

	engine := SearchEngine{ID: "google-news", Name: "Google News", Kind: "rss", Category: "news"}
	results, err := parseRSSEngine(strings.NewReader(feed), engine, "php release", 10)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	domains := map[string]bool{}
	for _, result := range results {
		domains[result.Domain] = true
	}
	if domains["news.google.com"] {
		t.Fatal("results were attributed to the aggregator instead of the real publisher")
	}
	if !domains["thehackernews.com"] || !domains["hostinger.com"] {
		t.Fatalf("expected real publisher domains, got %v", domains)
	}
}

func TestParseJSONEngineBuildsURLsFromTitlePrefix(t *testing.T) {
	// Wikipedia's search API returns titles with no URLs, so the engine
	// config supplies a prefix to build them.
	payload := `{"query":{"search":[
	  {"title":"Boiling point","snippet":"The boiling point of a substance"},
	  {"title":"Boiling","snippet":"Boiling is the rapid phase transition"}
	]}}`
	engine := SearchEngine{
		ID: "wikipedia", Name: "Wikipedia", Kind: "json", Category: "reference",
		ResultsPath: "query.search", TitleField: "title", DescField: "snippet",
		URLPrefix: "https://en.wikipedia.org/wiki/",
	}
	results, err := parseJSONEngine(strings.NewReader(payload), engine, "boiling point", 10)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if !strings.HasPrefix(results[0].URL, "https://en.wikipedia.org/wiki/") {
		t.Fatalf("expected a built wiki URL, got %q", results[0].URL)
	}
}

func TestLookupJSONArrayWalksDottedPath(t *testing.T) {
	payload := map[string]any{"query": map[string]any{"search": []any{map[string]any{"title": "x"}}}}
	if _, ok := lookupJSONArray(payload, "query.search"); !ok {
		t.Fatal("expected to resolve query.search")
	}
	if _, ok := lookupJSONArray(payload, "query.missing"); ok {
		t.Fatal("expected a missing path to fail rather than panic")
	}
	if _, ok := lookupJSONArray(payload, ""); ok {
		t.Fatal("expected an empty path to fail")
	}
}

func TestBuildSearchURLEscapesQuery(t *testing.T) {
	got := buildSearchURL("https://example.com/s?q={query}", "php 8.5 & more")
	if strings.Contains(got, " ") || strings.Contains(got, "&more") {
		t.Fatalf("query was not escaped: %q", got)
	}
	if !strings.Contains(got, "php") {
		t.Fatalf("query missing from URL: %q", got)
	}
}

func TestSourceDomainFallsBackWhenUnparseable(t *testing.T) {
	if got := sourceDomain("https://www.Example.com/page", "fallback"); got != "example.com" {
		t.Fatalf("expected normalized host, got %q", got)
	}
	if got := sourceDomain("", "bing"); got != "bing" {
		t.Fatalf("expected fallback for an empty URL, got %q", got)
	}
}

// TestShortSubjectTokensSurviveTokenization locks in the live-found bug: the
// old 4-character minimum silently discarded the one word that actually
// identified the subject, leaving only generic terms to match on.
func TestShortSubjectTokensSurviveTokenization(t *testing.T) {
	cases := map[string]string{
		"latest PHP release": "php",
		"npm install fails":  "npm",
		"git rebase help":    "git",
		"vue router setup":   "vue",
		"aws s3 upload":      "aws",
	}
	for query, expected := range cases {
		tokens := significantSearchTokens(query)
		found := false
		for _, token := range tokens {
			if token == expected {
				found = true
			}
		}
		if !found {
			t.Errorf("query %q lost its subject token %q (got %v)", query, expected, tokens)
		}
	}
}

func TestCoreTokensExcludeGenericWords(t *testing.T) {
	core := coreSearchTokens("latest PHP release")
	if len(core) != 1 || core[0] != "php" {
		t.Fatalf("expected only the subject token to be core, got %v", core)
	}
}

// TestRelevanceRejectsGenericOnlyMatches reproduces the exact live false
// positive: a GOV.UK story about scam images matched "latest PHP release"
// because it contained "releases" and "latest" while "php" had been dropped.
// Two such unrelated results were then scored as corroborating sources.
func TestRelevanceRejectsGenericOnlyMatches(t *testing.T) {
	candidates := []internetSearchResult{
		{Title: "DVLA releases latest scam images to help keep motorists safe online", Description: "GOV.UK"},
		{Title: "PHP 8.5 released", Description: "The latest PHP release is now available"},
	}
	results := filterRelevantResults("latest PHP release", candidates)
	if len(results) != 1 {
		t.Fatalf("expected only the on-topic result, got %d: %+v", len(results), results)
	}
	if !strings.Contains(results[0].Title, "PHP") {
		t.Fatalf("kept the wrong result: %q", results[0].Title)
	}
}

func TestShortTokensMatchOnWordBoundaryOnly(t *testing.T) {
	// "go" must not match inside "algorithm" / "google" / "going".
	if fuzzyTokenMatch("go", "a fast sorting algorithm explained") {
		t.Error(`"go" should not match inside "algorithm"`)
	}
	if fuzzyTokenMatch("go", "going to google today") {
		t.Error(`"go" should not match inside "going"/"google"`)
	}
	if !fuzzyTokenMatch("go", "the go programming language") {
		t.Error(`"go" should match as a whole word`)
	}
	if !fuzzyTokenMatch("php", "php 8.5 released") {
		t.Error(`"php" should match as a whole word`)
	}
}

func TestShouldEscalateForVerification(t *testing.T) {
	const confident = "PHP 8.5 was released with a new pipe operator."
	const honest = "The results don't contain information about that."

	cases := []struct {
		name        string
		stepErrored bool
		confidence  string
		paused      bool
		content     string
		expected    bool
	}{
		{name: "low confidence with a confident answer escalates", confidence: "low", content: confident, expected: true},
		{name: "high confidence needs no verification", confidence: "high", content: confident, expected: false},
		{name: "medium confidence needs no verification", confidence: "medium", content: confident, expected: false},
		{name: "no search ran at all", confidence: "", content: confident, expected: false},
		{name: "errored run has nothing to verify", stepErrored: true, confidence: "low", content: confident, expected: false},
		{name: "already asking the user something else", confidence: "low", paused: true, content: confident, expected: false},
		{name: "model already said the results were not usable", confidence: "low", content: honest, expected: false},
		{name: "empty answer", confidence: "low", content: "   ", expected: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := shouldEscalateForVerification(testCase.stepErrored, testCase.confidence, testCase.paused, testCase.content)
			if got != testCase.expected {
				t.Fatalf("expected %v, got %v", testCase.expected, got)
			}
		})
	}
}

func TestLoadSearchEnginesProtectsBuiltinsFromBadOverrides(t *testing.T) {
	// A builtin may be toggled/renamed from Settings, but its URL and parser
	// stay fixed so a malformed saved value can't break a known-good default.
	withSettingsFile(t, `{"searchEngines":[
	  {"id":"bing","enabled":false,"url_template":"https://evil.example/{query}","kind":"json"},
	  {"id":"custom","name":"Mine","enabled":true,"kind":"rss","url_template":"https://mine.example/?q={query}"},
	  {"id":"broken","name":"NoPlaceholder","enabled":true,"kind":"rss","url_template":"https://nope.example/"}
	]}`)

	engines := loadSearchEngines()
	byID := map[string]SearchEngine{}
	for _, engine := range engines {
		byID[engine.ID] = engine
	}

	bing, ok := byID["bing"]
	if !ok {
		t.Fatal("builtin bing disappeared")
	}
	if bing.Enabled {
		t.Fatal("expected the saved disabled flag to apply to the builtin")
	}
	if !strings.Contains(bing.URLTemplate, "bing.com") || bing.Kind != "rss" {
		t.Fatalf("builtin URL/kind was overridden by settings: %+v", bing)
	}

	if custom, ok := byID["custom"]; !ok || custom.Builtin {
		t.Fatalf("expected a user engine to be added and marked non-builtin: %+v", custom)
	}
	if _, ok := byID["broken"]; ok {
		t.Fatal("expected an engine without a {query} placeholder to be rejected")
	}
}
