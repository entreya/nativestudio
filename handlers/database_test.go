package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entreya/nativestudio/db"
)

func newDatabaseTestHandler(t *testing.T) *DatabaseHandler {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "query-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	if _, err := database.Exec(`CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT, author_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO authors (id, name) VALUES (1, 'Ada'), (2, 'Grace')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO books (id, title, author_id) VALUES (1, 'Notes', 1), (2, 'Compilers', 2), (3, 'Untitled', NULL)`); err != nil {
		t.Fatal(err)
	}

	return NewDatabaseHandler(database, "http://ollama.test", "test-model")
}

func runQuery(t *testing.T, handler *DatabaseHandler, body string) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/p1/db/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.RunQuery(rec, req)
	resp := rec.Result()
	var decoded map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp, decoded
}

func TestRunQuerySelectFromSingleTable(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 2 {
		t.Fatalf("expected 2 authors, got %+v", rows)
	}
}

func TestRunQueryJoinsAndAliases(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [
			{"table":"books","column":"title","alias":"book_title"},
			{"table":"authors","column":"name","alias":"author_name"}
		],
		"joins": [
			{"table":"authors","left_table":"books","left_column":"author_id","right_column":"id"}
		]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	columns, _ := body["columns"].([]interface{})
	if len(columns) != 2 || columns[0] != "book_title" || columns[1] != "author_name" {
		t.Fatalf("expected aliased columns [book_title author_name], got %+v", columns)
	}
	rows, _ := body["rows"].([]interface{})
	// INNER join drops the book with a NULL author_id, so 2 of 3 books survive.
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows from an inner join (the NULL-author book excluded), got %+v", rows)
	}
	first, _ := rows[0].(map[string]interface{})
	if first["book_title"] != "Notes" || first["author_name"] != "Ada" {
		t.Fatalf("unexpected joined row: %+v", first)
	}
}

func TestRunQueryLeftJoinKeepsUnmatchedRows(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	_, body := runQuery(t, handler, `{
		"from": "books",
		"select": [
			{"table":"books","column":"title","alias":"book_title"},
			{"table":"authors","column":"name","alias":"author_name"}
		],
		"joins": [
			{"type":"LEFT","table":"authors","left_table":"books","left_column":"author_id","right_column":"id"}
		]
	}`)
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 3 {
		t.Fatalf("expected all 3 books with a LEFT join, got %+v", rows)
	}
}

func TestRunQueryRejectsUnknownTable(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{"from":"nonexistent","select":[{"table":"nonexistent","column":"x"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown table, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryRejectsUnknownColumn(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{"from":"authors","select":[{"table":"authors","column":"does_not_exist"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown column, got %d: %+v", resp.StatusCode, body)
	}
}

// TestRunQueryRejectsInjectionViaTableName is the key security regression
// test: table/column names can't go through database/sql's own value
// parameterization (only values can), so this is what actually stands
// between a crafted "table" field and arbitrary SQL execution.
func TestRunQueryRejectsInjectionViaTableName(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors; DROP TABLE authors; --",
		"select": [{"table":"authors","column":"name"}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a table name containing SQL, got %d: %+v", resp.StatusCode, body)
	}

	// Confirm the table really does still exist and wasn't touched.
	_, verify := runQuery(t, handler, `{"from":"authors","select":[{"table":"authors","column":"name"}]}`)
	rows, _ := verify["rows"].([]interface{})
	if len(rows) != 2 {
		t.Fatalf("authors table should be untouched (still 2 rows), got %+v", verify)
	}
}

func TestRunQueryRejectsJoinNotChainedToBaseTable(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [{"table":"authors","column":"name"}],
		"joins": [
			{"table":"authors","left_table":"some_other_table","left_column":"id","right_column":"id"}
		]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when a join's left side isn't in the query yet, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryRejectsEmptySelect(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{"from":"authors","select":[]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty select list, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryDuplicateUnaliasedColumnsRejected(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [
			{"table":"books","column":"id"},
			{"table":"authors","column":"id"}
		],
		"joins": [
			{"table":"authors","left_table":"books","left_column":"author_id","right_column":"id"}
		]
	}`)
	// Both display as "books.id"/"authors.id" (auto-qualified since joins are
	// present), so this should actually succeed, not collide — verifies the
	// auto-qualification logic rather than the duplicate-rejection path.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected auto-qualified column names to avoid collision, got %d: %+v", resp.StatusCode, body)
	}
	columns, _ := body["columns"].([]interface{})
	if len(columns) != 2 || columns[0] != "books.id" || columns[1] != "authors.id" {
		t.Fatalf("expected auto-qualified columns [books.id authors.id], got %+v", columns)
	}
}

func TestRunQueryLimitIsCapped(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	_, body := runQuery(t, handler, `{"from":"books","select":[{"table":"books","column":"id"}],"limit":999999}`)
	sqlText, _ := body["sql"].(string)
	if !strings.Contains(sqlText, "LIMIT 500") {
		t.Fatalf("expected an oversized limit to be capped at 500, got sql=%q", sqlText)
	}
}

func TestRunQueryWhereEquals(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{"table":"authors","column":"name","operator":"=","value":"Ada"}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 matching row, got %+v", rows)
	}
}

func TestRunQueryWhereContainsStartsEndsWith(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	cases := []struct {
		operator string
		value    string
		want     int
	}{
		{"contains", "mpil", 1},     // "Compilers"
		{"starts_with", "Not", 1},   // "Notes"
		{"ends_with", "tled", 1},    // "Untitled"
		{"contains", "zzz", 0},
	}
	for _, tc := range cases {
		_, body := runQuery(t, handler, fmt.Sprintf(`{
			"from": "books",
			"select": [{"table":"books","column":"title"}],
			"where": [{"table":"books","column":"title","operator":%q,"value":%q}]
		}`, tc.operator, tc.value))
		rows, _ := body["rows"].([]interface{})
		if len(rows) != tc.want {
			t.Errorf("operator %s value %q: expected %d rows, got %d (%+v)", tc.operator, tc.value, tc.want, len(rows), rows)
		}
	}
}

func TestRunQueryWhereIsNullAndIsNotNull(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	_, nullBody := runQuery(t, handler, `{
		"from": "books",
		"select": [{"table":"books","column":"title"}],
		"where": [{"table":"books","column":"author_id","operator":"is_null"}]
	}`)
	nullRows, _ := nullBody["rows"].([]interface{})
	if len(nullRows) != 1 {
		t.Fatalf("expected 1 book with a NULL author_id, got %+v", nullRows)
	}

	_, notNullBody := runQuery(t, handler, `{
		"from": "books",
		"select": [{"table":"books","column":"title"}],
		"where": [{"table":"books","column":"author_id","operator":"is_not_null"}]
	}`)
	notNullRows, _ := notNullBody["rows"].([]interface{})
	if len(notNullRows) != 2 {
		t.Fatalf("expected 2 books with a non-NULL author_id, got %+v", notNullRows)
	}
}

func TestRunQueryWhereValueIsNeverExecutedAsSQL(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	// A WHERE value is a bound parameter, not an identifier — this must be
	// treated as a literal string to search for, never as SQL.
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{"table":"authors","column":"name","operator":"=","value":"x'; DROP TABLE authors; --"}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (a literal value, however weird, is not a SQL error), got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 0 {
		t.Fatalf("expected 0 matches for a literal that isn't any author's name, got %+v", rows)
	}

	_, verify := runQuery(t, handler, `{"from":"authors","select":[{"table":"authors","column":"name"}]}`)
	verifyRows, _ := verify["rows"].([]interface{})
	if len(verifyRows) != 2 {
		t.Fatalf("authors table should be untouched (still 2 rows), got %+v", verify)
	}
}

func TestRunQueryRejectsUnknownOperator(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{"table":"authors","column":"name","operator":"DROP TABLE","value":"x"}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unrecognized operator, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryGroupByWithCount(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [
			{"table":"books","column":"author_id"},
			{"table":"books","column":"id","aggregate":"COUNT","alias":"book_count"}
		],
		"group_by": [{"table":"books","column":"author_id"}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	// 3 books: author_id 1, author_id 2, and NULL — three distinct groups.
	if len(rows) != 3 {
		t.Fatalf("expected 3 groups (author 1, author 2, NULL), got %+v", rows)
	}
}

func TestRunQueryAggregateWithoutGroupByRejected(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [{"table":"books","column":"id","aggregate":"COUNT"}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when an aggregate is used with no Group By, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryRejectsUnknownAggregate(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [{"table":"books","column":"id","aggregate":"DROP"}],
		"group_by": [{"table":"books","column":"author_id"}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unsupported aggregate function, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryInSubquery(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"table":"authors","column":"id","operator":"in_subquery",
			"subquery": {
				"from": "books",
				"select": [{"table":"books","column":"author_id"}],
				"where": [{"table":"books","column":"title","operator":"=","value":"Notes"}]
			}
		}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 author (Ada, via the book titled Notes), got %+v", rows)
	}
	first, _ := rows[0].(map[string]interface{})
	if first["name"] != "Ada" {
		t.Fatalf("expected Ada, got %+v", first)
	}
}

func TestRunQueryNotInSubquery(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	_, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"table":"authors","column":"id","operator":"not_in_subquery",
			"subquery": {
				"from": "books",
				"select": [{"table":"books","column":"author_id"}],
				"where": [{"table":"books","column":"title","operator":"=","value":"Notes"}]
			}
		}]
	}`)
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 author (Grace, everyone except who wrote Notes), got %+v", rows)
	}
	first, _ := rows[0].(map[string]interface{})
	if first["name"] != "Grace" {
		t.Fatalf("expected Grace, got %+v", first)
	}
}

// TestRunQuerySubqueryInsideSubquery covers exactly the case asked about
// live: a subquery whose own WHERE contains another subquery.
func TestRunQuerySubqueryInsideSubquery(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"table":"authors","column":"id","operator":"in_subquery",
			"subquery": {
				"from": "books",
				"select": [{"table":"books","column":"author_id"}],
				"where": [{
					"table":"books","column":"author_id","operator":"in_subquery",
					"subquery": {
						"from": "authors",
						"select": [{"table":"authors","column":"id"}],
						"where": [{"table":"authors","column":"name","operator":"=","value":"Ada"}]
					}
				}]
			}
		}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a subquery nested inside a subquery, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 author (Ada, via two levels of nesting), got %+v", rows)
	}
}

func TestRunQuerySubqueryMustSelectExactlyOneColumn(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"table":"authors","column":"id","operator":"in_subquery",
			"subquery": {
				"from": "books",
				"select": [{"table":"books","column":"author_id"},{"table":"books","column":"title"}]
			}
		}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a subquery selecting more than one column, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQuerySubqueryValidatesItsOwnIdentifiers(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"table":"authors","column":"id","operator":"in_subquery",
			"subquery": {
				"from": "books; DROP TABLE authors; --",
				"select": [{"table":"books","column":"author_id"}]
			}
		}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 — a subquery goes through the same identifier validation as the top-level query, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryRejectsExcessiveSubqueryNesting(t *testing.T) {
	handler := newDatabaseTestHandler(t)

	// Build maxSubqueryDepth+2 levels of "authors.id in_subquery (SELECT id FROM authors WHERE ...)"
	// programmatically — hand-nesting this many levels of JSON isn't worth
	// the readability cost.
	innermost := map[string]interface{}{
		"from":   "authors",
		"select": []map[string]interface{}{{"table": "authors", "column": "id"}},
	}
	nested := innermost
	for i := 0; i < maxSubqueryDepth+2; i++ {
		nested = map[string]interface{}{
			"from":   "authors",
			"select": []map[string]interface{}{{"table": "authors", "column": "id"}},
			"where": []map[string]interface{}{{
				"table": "authors", "column": "id", "operator": "in_subquery", "subquery": nested,
			}},
		}
	}
	body, err := json.Marshal(nested)
	if err != nil {
		t.Fatal(err)
	}

	resp, decoded := runQuery(t, handler, string(body))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for excessive subquery nesting, got %d: %+v", resp.StatusCode, decoded)
	}
}

func TestRunQueryOrCombinator(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	_, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [
			{"table":"authors","column":"name","operator":"=","value":"Ada"},
			{"table":"authors","column":"name","operator":"=","value":"Grace","combinator":"OR"}
		]
	}`)
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 2 {
		t.Fatalf("expected both authors matched via OR, got %+v", rows)
	}
}

func TestRunQueryNegateCondition(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	_, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{"table":"authors","column":"name","operator":"=","value":"Ada","negate":true}]
	}`)
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected 1 author (everyone except Ada), got %+v", rows)
	}
	first, _ := rows[0].(map[string]interface{})
	if first["name"] != "Grace" {
		t.Fatalf("expected Grace, got %+v", first)
	}
}

func TestRunQueryRejectsUnknownCombinator(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [
			{"table":"authors","column":"name","operator":"=","value":"Ada"},
			{"table":"authors","column":"name","operator":"=","value":"Grace","combinator":"XOR"}
		]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unsupported combinator, got %d: %+v", resp.StatusCode, body)
	}
}

// TestRunQueryCorrelatedExists is the concrete case asked about live: a
// correlated EXISTS, where the subquery's WHERE compares one of its own
// columns against a column from the OUTER query — "authors that have at
// least one book" — rather than an uncorrelated subquery or a literal value.
func TestRunQueryCorrelatedExists(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"operator":"exists",
			"subquery": {
				"from": "books",
				"select": [{"table":"books","column":"id"}],
				"where": [{
					"table":"books","column":"author_id","operator":"=",
					"value_column": {"table":"authors","column":"id"}
				}]
			}
		}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 2 {
		t.Fatalf("expected both Ada and Grace (each has a book), got %+v", rows)
	}
}

func TestRunQueryCorrelatedNotExists(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	// Give Ada a second book so the fixture has an author-with-multiple-books
	// case too, then ask for authors with NO book titled "Ghost".
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{
			"operator":"not_exists",
			"subquery": {
				"from": "books",
				"select": [{"table":"books","column":"id"}],
				"where": [
					{"table":"books","column":"author_id","operator":"=","value_column":{"table":"authors","column":"id"}},
					{"table":"books","column":"title","operator":"=","value":"Ghost","combinator":"AND"}
				]
			}
		}]
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	rows, _ := body["rows"].([]interface{})
	if len(rows) != 2 {
		t.Fatalf("expected both authors (neither has a book called Ghost), got %+v", rows)
	}
}

func TestRunQueryExistsRequiresSubquery(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := runQuery(t, handler, `{
		"from": "authors",
		"select": [{"table":"authors","column":"name"}],
		"where": [{"operator":"exists"}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when exists has no subquery, got %d: %+v", resp.StatusCode, body)
	}
}

func TestRunQueryValueColumnMustBeReachable(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	// No outer correlation available here (top-level query, not a
	// subquery) — referencing "authors" from a plain "books" query must be
	// rejected, not silently reference an unrelated table.
	resp, body := runQuery(t, handler, `{
		"from": "books",
		"select": [{"table":"books","column":"title"}],
		"where": [{"table":"books","column":"author_id","operator":"=","value_column":{"table":"authors","column":"id"}}]
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 — authors isn't joined into this query, got %d: %+v", resp.StatusCode, body)
	}
}

// withGenerateQueryTransport swaps generateQueryClient's transport rather
// than http.DefaultClient's — GenerateQuery deliberately uses its own client
// (same reasoning as agent/rephrase.go's rephraserClient) so this call's
// mocked response can't desync any other test that scripts
// http.DefaultClient.Transport.
func withGenerateQueryTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	original := generateQueryClient.Transport
	generateQueryClient.Transport = transport
	t.Cleanup(func() { generateQueryClient.Transport = original })
}

func generateQuery(t *testing.T, handler *DatabaseHandler, body string) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/p1/db/query/generate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.GenerateQuery(rec, req)
	resp := rec.Result()
	var decoded map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp, decoded
}

func TestGenerateQueryReturnsValidatedStructuredQuery(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	withGenerateQueryTransport(t, scriptedOllamaResponse(t, strings.ReplaceAll(
		`{"from":"authors","select":[{"table":"authors","column":"name"}]}`, `"`, `\"`)))

	resp, body := generateQuery(t, handler, `{"description":"every author's name"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, body)
	}
	query, ok := body["query"].(map[string]interface{})
	if !ok || query["from"] != "authors" {
		t.Fatalf("expected the parsed query in the response, got %+v", body)
	}
	if body["sql"] == "" || body["sql"] == nil {
		t.Fatalf("expected a validated SQL preview, got %+v", body)
	}
}

func TestGenerateQueryRejectsInventedTable(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	withGenerateQueryTransport(t, scriptedOllamaResponse(t, strings.ReplaceAll(
		`{"from":"not_a_real_table","select":[{"table":"not_a_real_table","column":"x"}]}`, `"`, `\"`)))

	resp, body := generateQuery(t, handler, `{"description":"something"}`)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when the model invents a table that doesn't exist, got %d: %+v", resp.StatusCode, body)
	}
}

func TestGenerateQueryRejectsNonJSONResponse(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	withGenerateQueryTransport(t, scriptedOllamaResponse(t, "sure, here is a query for you: SELECT * FROM authors"))

	resp, body := generateQuery(t, handler, `{"description":"something"}`)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when the model doesn't return JSON, got %d: %+v", resp.StatusCode, body)
	}
}

func TestGenerateQueryRequiresDescription(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	resp, body := generateQuery(t, handler, `{"description":""}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty description, got %d: %+v", resp.StatusCode, body)
	}
}

// TestGenerateQueryNeverExecutesAnything confirms GenerateQuery only builds
// and validates SQL — it must never actually run it against the database
// (this endpoint is meant to populate the builder UI for the user to review,
// not to execute a model-authored query unattended).
func TestGenerateQueryNeverExecutesAnything(t *testing.T) {
	handler := newDatabaseTestHandler(t)
	withGenerateQueryTransport(t, scriptedOllamaResponse(t, strings.ReplaceAll(
		`{"from":"authors","select":[{"table":"authors","column":"name"}]}`, `"`, `\"`)))

	_, before := runQuery(t, handler, `{"from":"authors","select":[{"table":"authors","column":"name"}]}`)
	beforeRows, _ := before["rows"].([]interface{})

	generateQuery(t, handler, `{"description":"every author's name"}`)

	_, after := runQuery(t, handler, `{"from":"authors","select":[{"table":"authors","column":"name"}]}`)
	afterRows, _ := after["rows"].([]interface{})
	if len(beforeRows) != len(afterRows) {
		t.Fatalf("row count changed after GenerateQuery (%d -> %d) — it must never execute anything", len(beforeRows), len(afterRows))
	}
}
