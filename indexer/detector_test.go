package indexer

import "testing"

func TestDetectFactsKeepsRouteAndDatabaseEvidence(t *testing.T) {
	file := ScannedFile{Path: "api.js", Hash: "hash", Content: []byte("router.post('/users', createUser)\nconst sql = `CREATE TABLE users (id int)`\n")}
	facts := DetectFacts(file)
	categories := map[string]bool{}
	for _, fact := range facts {
		categories[fact.Category] = true
		if len(fact.Sources) != 1 || fact.Sources[0].Path != "api.js" || fact.Sources[0].ContentHash != "hash" || fact.Sources[0].StartLine < 1 {
			t.Fatalf("fact lost evidence: %#v", fact)
		}
	}
	if !categories["route"] || !categories["database_entity"] {
		t.Fatalf("expected route and database facts, got %#v", facts)
	}
}
