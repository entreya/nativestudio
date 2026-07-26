package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entreya/nativestudio/db"
)

type DatabaseHandler struct {
	db           *db.DB
	ollamaURL    string
	defaultModel string
}

func NewDatabaseHandler(database *db.DB, ollamaURL, defaultModel string) *DatabaseHandler {
	return &DatabaseHandler{db: database, ollamaURL: ollamaURL, defaultModel: defaultModel}
}

func (h *DatabaseHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/db/tables", h.GetTables)
	mux.HandleFunc("GET /api/projects/{id}/db/tables/{table}/data", h.GetTableData)
	mux.HandleFunc("POST /api/projects/{id}/db/query/generate", h.GenerateQuery)
	mux.HandleFunc("POST /api/projects/{id}/db/query", h.RunQuery)
}

func (h *DatabaseHandler) GetTables(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tables = append(tables, name)
	}

	if tables == nil {
		tables = []string{}
	}

	writeJSON(w, tables)
}

func (h *DatabaseHandler) GetTableData(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	// 1. Get schema
	schemaRows, err := h.db.QueryContext(r.Context(), fmt.Sprintf("PRAGMA table_info(%q)", tableName))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer schemaRows.Close()

	type ColumnDef struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var columns []ColumnDef
	var colNames []string

	for schemaRows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt_value interface{}
		var pk int
		if err := schemaRows.Scan(&cid, &name, &ctype, &notnull, &dflt_value, &pk); err != nil {
			continue
		}
		columns = append(columns, ColumnDef{Name: name, Type: ctype})
		colNames = append(colNames, name)
	}
	schemaRows.Close()

	if len(columns) == 0 {
		http.Error(w, "table not found or has no columns", http.StatusNotFound)
		return
	}

	// 2. Total row count, so the UI can say how much of the table it is
	// showing. Without this the explorer silently displayed the first page
	// and reported it as the row count — a 27,936-row table looked like it
	// had 100 rows.
	var totalRows int64
	if err := h.db.QueryRowContext(r.Context(), fmt.Sprintf("SELECT COUNT(*) FROM %q", tableName)).Scan(&totalRows); err != nil {
		totalRows = 0
	}

	// 3. Get the requested page.
	limit := parsePositiveQueryInt(r, "limit", databasePageSize, maxDatabasePageSize)
	offset := parsePositiveQueryInt(r, "offset", 0, 0)

	dataRows, err := h.db.QueryContext(r.Context(),
		fmt.Sprintf("SELECT * FROM %q LIMIT %d OFFSET %d", tableName, limit, offset))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dataRows.Close()

	rows, err := scanRowsToMaps(dataRows, colNames)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"columns":    columns,
		"rows":       rows,
		"total_rows": totalRows,
		"limit":      limit,
		"offset":     offset,
	})
}

// scanRowsToMaps reads every remaining row from dataRows into a
// column-name-keyed map, converting the []byte SQLite hands back for TEXT
// columns into a plain string so it serializes as a JSON string rather than
// a base64 blob. Shared by GetTableData and RunQuery so both read rows the
// same way.
func scanRowsToMaps(dataRows interface {
	Next() bool
	Scan(...interface{}) error
}, colNames []string) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}
	for dataRows.Next() {
		columnData := make([]interface{}, len(colNames))
		columnPointers := make([]interface{}, len(colNames))
		for i := range columnData {
			columnPointers[i] = &columnData[i]
		}

		if err := dataRows.Scan(columnPointers...); err != nil {
			continue
		}

		row := make(map[string]interface{})
		for i, colName := range colNames {
			val := columnData[i]
			if b, ok := val.([]byte); ok {
				row[colName] = string(b)
			} else {
				row[colName] = val
			}
		}
		rows = append(rows, row)
	}

	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return rows, nil
}

const (
	databasePageSize    = 100
	maxDatabasePageSize = 500
)

// identifierPattern is the only shape a table or column name (and alias) is
// ever allowed to have before being interpolated into a query. database/sql
// only parameterizes values, never identifiers, so table/column names in
// RunQuery can't go through the driver's placeholder mechanism the way a
// value would — this pattern plus the sqlite_master/PRAGMA table_info
// membership check below (belt and suspenders) is what stands in for that.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type queryColumnRef struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Alias  string `json:"alias,omitempty"`
	// Aggregate wraps the column in COUNT/SUM/AVG/MIN/MAX when set — only
	// meaningful (and only allowed) alongside GroupBy, matching how GROUP BY
	// is actually used: some columns to group by, others summarized per group.
	Aggregate string `json:"aggregate,omitempty"`
}

type queryJoin struct {
	Type        string `json:"type"` // "INNER" or "LEFT"; defaults to INNER
	Table       string `json:"table"`
	LeftTable   string `json:"left_table"`
	LeftColumn  string `json:"left_column"`
	RightColumn string `json:"right_column"`
}

type queryWhere struct {
	// Table/Column are unused (and may be omitted) for "exists"/"not_exists" —
	// those don't compare an outer column against anything, they only check
	// whether the subquery has any matching rows.
	Table    string `json:"table,omitempty"`
	Column   string `json:"column,omitempty"`
	Operator string `json:"operator"` // see whereOperators below
	Value    string `json:"value,omitempty"`
	// Subquery is set instead of Value for "in_subquery"/"not_in_subquery"/
	// "exists"/"not_exists" — a nested query builder request, validated and
	// built the same way as the top-level one (buildQuerySQL calls itself
	// recursively), so a subquery's own WHERE can itself contain another
	// subquery. For exists/not_exists specifically, the subquery may
	// correlate back to this level's tables (e.g. "books.author_id =
	// authors.id") — see the outerTables parameter on buildQuerySQL.
	Subquery *queryRequest `json:"subquery,omitempty"`
	// ValueColumn, when set, compares Table.Column against ANOTHER column
	// instead of a literal Value — required to write a correlated condition
	// like "books.author_id = authors.id" inside an exists/not_exists
	// subquery (without it, Value can only ever be a bound literal, which
	// makes correlation — the useful form of EXISTS — impossible to express).
	// Its Table must be reachable from either this level's own FROM/JOINs or
	// the outer query's, when this condition sits inside a subquery.
	ValueColumn *queryColumnRef `json:"value_column,omitempty"`
	// Negate wraps this condition's SQL in NOT (...).
	Negate bool `json:"negate,omitempty"`
	// Combinator joins this condition to the PREVIOUS one — "AND" or "OR",
	// defaulting to "AND". Ignored on the first condition in the list.
	// Conditions are combined strictly left to right in list order using
	// ordinary SQL operator precedence (AND binds tighter than OR) — the
	// same semantics as writing "a AND b OR c" by hand — rather than
	// supporting arbitrarily nested parenthesized groups.
	Combinator string `json:"combinator,omitempty"`
}

type queryRequest struct {
	From    string           `json:"from"`
	Select  []queryColumnRef `json:"select"`
	Joins   []queryJoin      `json:"joins"`
	Where   []queryWhere     `json:"where"`
	GroupBy []queryColumnRef `json:"group_by"`
	Limit   int              `json:"limit"`
}

// maxSubqueryDepth bounds how many levels of "subquery inside a subquery"
// buildQuerySQL will follow before refusing — a generous ceiling for any
// query a human would actually construct by hand in the builder UI, and a
// backstop against a pathological or malformed request recursing without end.
const maxSubqueryDepth = 5

// whereOperators maps each operator the query builder UI can offer to the
// SQL it produces. LIKE-family operators wrap the bound value with wildcards
// here (never in the client), and is_null/is_not_null take no value at all —
// both are handled in buildWhereClause, not by trusting the operator string
// to already be valid SQL. in_subquery/not_in_subquery take a nested
// queryRequest (see queryWhere.Subquery) instead of either.
var whereOperators = map[string]string{
	"=": "=", "!=": "!=", "<": "<", "<=": "<=", ">": ">", ">=": ">=",
	"contains": "LIKE", "starts_with": "LIKE", "ends_with": "LIKE",
	"in_subquery": "IN", "not_in_subquery": "NOT IN",
	"is_null": "IS NULL", "is_not_null": "IS NOT NULL",
	"exists": "EXISTS", "not_exists": "NOT EXISTS",
}

// noColumnOperators don't compare an outer table.column against anything, so
// queryWhere.Table/Column are not required (and ignored if present) for
// these — the condition is just "does the subquery have any matching rows".
var noColumnOperators = map[string]bool{"exists": true, "not_exists": true}

var aggregateFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true,
}

// RunQuery builds and executes a SELECT from a structured description
// (base table, joins, selected columns/aliases) rather than accepting raw
// SQL from the client. Every table and column name is checked against the
// database's real schema before being used, and joins are validated as a
// linear chain — each join's left side must be the base table or an
// earlier join's table, so a query can't reference a table it never
// actually joined in.
// generateQueryTimeout bounds the NL-to-query call — it's a one-shot JSON
// generation, not a conversation, so it doesn't need the agent's own
// multi-minute step budget.
const generateQueryTimeout = 30 * time.Second

// generateQueryClient is its own http.Client for the same reason
// agent/rephrase.go's rephraserClient is: keeping this call's network traffic
// independent of anything that mocks http.DefaultClient's transport in tests.
var generateQueryClient = &http.Client{Timeout: generateQueryTimeout}

const generateQuerySystemPrompt = `You translate a plain-English description of what data someone wants into a single JSON object describing a read-only query. Output ONLY that JSON object — no prose, no markdown fences, no explanation.

The JSON shape:
{
  "from": "table_name",
  "select": [{"table": "table_name", "column": "column_name", "alias": "optional", "aggregate": "optional: COUNT|SUM|AVG|MIN|MAX"}],
  "joins": [{"type": "INNER|LEFT", "table": "table_name", "left_table": "table_name", "left_column": "column_name", "right_column": "column_name"}],
  "where": [{"table": "table_name", "column": "column_name", "operator": "=|!=|<|<=|>|>=|contains|starts_with|ends_with|is_null|is_not_null", "value": "string", "combinator": "AND|OR", "negate": false}],
  "group_by": [{"table": "table_name", "column": "column_name"}],
  "limit": 100
}

Rules:
- Only use table and column names that actually appear in the schema given below. Never invent one.
- "select" must have at least one entry.
- Every mentioned filter, condition, or restriction in the description MUST become a "where" entry — do not drop one just because "select" and "from" alone would still produce valid JSON. A query missing a filter the user asked for is wrong even if it "runs".
- Every join must chain from "from" or an earlier join — its "left_table" must already be reachable.
- Use "aggregate" on a select column only when "group_by" is non-empty.
- Omit "joins", "where", "group_by" entirely (or use an empty list) when not needed. Omit "alias", "aggregate", "combinator", "negate" fields when not needed rather than sending empty strings.
- The first "where" entry's "combinator" is ignored (there's nothing before it to combine with).

Example. Schema:
books(id, title, author_id)
authors(id, name)
Description: titles of books by an author named Ada, newest 10
Output:
{"from":"books","select":[{"table":"books","column":"title"}],"joins":[{"type":"INNER","table":"authors","left_table":"books","left_column":"author_id","right_column":"id"}],"where":[{"table":"authors","column":"name","operator":"=","value":"Ada"}],"limit":10}`

// GenerateQuery turns a plain-English description into a structured
// queryRequest using the app's own configured model — not a dedicated
// text-to-SQL model. It never executes anything: the model's output is
// parsed and run through the exact same buildQuerySQL validation RunQuery
// uses (confirming it's real before it's returned), then handed back as
// data for the query-builder UI to populate, the same way a rename_symbol
// result is staged rather than applied — the user still reviews and
// presses Run themselves.
func (h *DatabaseHandler) GenerateQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string `json:"description"`
		Model       string `json:"model,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		http.Error(w, "description is required", http.StatusBadRequest)
		return
	}
	model := req.Model
	if model == "" {
		model = h.defaultModel
	}
	if model == "" {
		http.Error(w, "no model configured", http.StatusInternalServerError)
		return
	}

	schema, err := h.loadSchema(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	schemaText := renderSchemaForPrompt(schema)

	reqCtx, cancel := context.WithTimeout(r.Context(), generateQueryTimeout)
	defer cancel()

	generated, err := callGenerateQueryModel(reqCtx, h.ollamaURL, model, schemaText, description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var parsedReq queryRequest
	if err := json.Unmarshal([]byte(generated), &parsedReq); err != nil {
		http.Error(w, fmt.Sprintf("model did not return valid JSON: %v", err), http.StatusUnprocessableEntity)
		return
	}

	// Validate it the same way RunQuery would — this is what stands between
	// "the model produced something that parses as JSON" and "the model
	// produced a query that's actually safe and correct to show the user as
	// a suggestion." An invalid result is reported as an error, not silently
	// patched or guessed at.
	sqlText, _, colAliases, err := buildQuerySQL(parsedReq, schema, 0, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("generated query is invalid: %v", err), http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, map[string]interface{}{
		"query":   parsedReq,
		"sql":     sqlText,
		"columns": colAliases,
	})
}

// renderSchemaForPrompt turns the validated schema map into a compact,
// deterministic text block for the model — sorted so the same schema always
// produces the same prompt text (easier to reason about/debug than map
// iteration order, which Go randomizes).
func renderSchemaForPrompt(schema tableSchema) string {
	tables := make([]string, 0, len(schema))
	for t := range schema {
		tables = append(tables, t)
	}
	sort.Strings(tables)

	var b strings.Builder
	for _, table := range tables {
		cols := make([]string, 0, len(schema[table]))
		for c := range schema[table] {
			cols = append(cols, c)
		}
		sort.Strings(cols)
		fmt.Fprintf(&b, "%s(%s)\n", table, strings.Join(cols, ", "))
	}
	return b.String()
}

// callGenerateQueryModel asks the model for the queryRequest JSON described
// in generateQuerySystemPrompt, using Ollama's "format": "json" mode so the
// model is constrained to emit valid JSON syntax (it can still emit the
// WRONG shape or invent table/column names — that's what buildQuerySQL's
// validation, run by the caller, catches).
func callGenerateQueryModel(ctx context.Context, ollamaURL, model, schemaText, description string) (string, error) {
	body := map[string]any{
		"model":      model,
		"stream":     false,
		"think":      false,
		"format":     "json",
		"keep_alive": "5m",
		"messages": []map[string]string{
			{"role": "system", "content": generateQuerySystemPrompt},
			{"role": "user", "content": fmt.Sprintf("Schema:\n%s\nDescription: %s", schemaText, description)},
		},
		"options": map[string]any{"temperature": 0.1},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := generateQueryClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("model unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model returned status %d", resp.StatusCode)
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode model response: %w", err)
	}
	content := strings.TrimSpace(parsed.Message.Content)
	if content == "" {
		return "", fmt.Errorf("model returned an empty response")
	}
	return content, nil
}

func (h *DatabaseHandler) RunQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	schema, err := h.loadSchema(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sqlText, args, colAliases, err := buildQuerySQL(req, schema, 0, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dataRows, err := h.db.QueryContext(r.Context(), sqlText, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dataRows.Close()

	rows, err := scanRowsToMaps(dataRows, colAliases)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"columns": colAliases,
		"rows":    rows,
		"sql":     renderDisplaySQL(sqlText, args),
	})
}

// renderDisplaySQL substitutes each "?" placeholder with a quoted literal of
// its bound argument, purely so the UI can show a human-readable query — the
// actual query already ran parameterized before this is ever called; this
// version is never itself executed.
func renderDisplaySQL(sqlText string, args []interface{}) string {
	var b strings.Builder
	argIndex := 0
	for i := 0; i < len(sqlText); i++ {
		if sqlText[i] == '?' && argIndex < len(args) {
			switch v := args[argIndex].(type) {
			case string:
				b.WriteString("'" + strings.ReplaceAll(v, "'", "''") + "'")
			default:
				fmt.Fprintf(&b, "%v", v)
			}
			argIndex++
			continue
		}
		b.WriteByte(sqlText[i])
	}
	return b.String()
}

// tableSchema maps table name -> set of its real column names, used to
// validate every identifier in a query request before it's interpolated.
type tableSchema map[string]map[string]bool

func (h *DatabaseHandler) loadSchema(ctx context.Context) (tableSchema, error) {
	tableRows, err := h.db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	schema := tableSchema{}
	var tableNames []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err == nil {
			tableNames = append(tableNames, name)
		}
	}
	tableRows.Close()

	for _, table := range tableNames {
		colRows, err := h.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
		if err != nil {
			continue
		}
		cols := map[string]bool{}
		for colRows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt interface{}
			if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				cols[name] = true
			}
		}
		colRows.Close()
		schema[table] = cols
	}
	return schema, nil
}

func validIdentifier(schema tableSchema, table, column string) bool {
	if !identifierPattern.MatchString(table) || !identifierPattern.MatchString(column) {
		return false
	}
	cols, ok := schema[table]
	return ok && cols[column]
}

// buildQuerySQL validates req against schema and returns the assembled,
// parameterized SQL (value placeholders as "?", never interpolated), its
// bound args in order, and the display name (alias, or "table.column" when
// ambiguous/unaliased) for each selected column. Every table/column name is
// checked against schema before being written into the query string —
// nothing here is interpolated from the request without first being
// confirmed to name a real table/column, so a request can't smuggle
// arbitrary SQL through an "identifier" field. WHERE values go through
// database/sql's own parameterization instead, since those genuinely are
// values, not identifiers.
//
// depth is 0 for the top-level request and incremented for each nested
// in_subquery/not_in_subquery/exists/not_exists — see maxSubqueryDepth.
// outerTables is the set of table names reachable from an enclosing query,
// when req is itself a subquery — nil for the top-level call. A WHERE
// condition (or its ValueColumn) may reference either req's own
// FROM/JOIN tables or outerTables, which is what makes a correlated
// EXISTS/NOT EXISTS possible.
func buildQuerySQL(req queryRequest, schema tableSchema, depth int, outerTables map[string]bool) (string, []interface{}, []string, error) {
	if depth > maxSubqueryDepth {
		return "", nil, nil, fmt.Errorf("subqueries nested too deeply (max %d levels)", maxSubqueryDepth)
	}
	if !identifierPattern.MatchString(req.From) {
		return "", nil, nil, fmt.Errorf("invalid table name %q", req.From)
	}
	if _, ok := schema[req.From]; !ok {
		return "", nil, nil, fmt.Errorf("unknown table %q", req.From)
	}
	if len(req.Select) == 0 {
		return "", nil, nil, fmt.Errorf("select list must not be empty")
	}
	if len(req.Joins) > 8 {
		return "", nil, nil, fmt.Errorf("too many joins (max 8)")
	}

	// joinedTables tracks every table already reachable from the FROM
	// clause, so each join's left side (and every select/where/group-by
	// column) can be checked against something that's actually in the
	// query, not just against the database as a whole.
	joinedTables := map[string]bool{req.From: true}
	// reachable reports whether table is valid to reference from a WHERE
	// condition at this level — either this query's own FROM/JOINs, or (for
	// a correlated subquery) the enclosing query's.
	reachable := func(table string) bool { return joinedTables[table] || outerTables[table] }
	var joinClauses []string

	for _, join := range req.Joins {
		joinType := strings.ToUpper(strings.TrimSpace(join.Type))
		if joinType == "" {
			joinType = "INNER"
		}
		if joinType != "INNER" && joinType != "LEFT" {
			return "", nil, nil, fmt.Errorf("unsupported join type %q (use INNER or LEFT)", join.Type)
		}
		if !identifierPattern.MatchString(join.Table) {
			return "", nil, nil, fmt.Errorf("invalid table name %q", join.Table)
		}
		if _, ok := schema[join.Table]; !ok {
			return "", nil, nil, fmt.Errorf("unknown table %q", join.Table)
		}
		if !joinedTables[join.LeftTable] {
			return "", nil, nil, fmt.Errorf("join references %q before it's joined — joins must chain from the base table or an earlier join", join.LeftTable)
		}
		if !validIdentifier(schema, join.LeftTable, join.LeftColumn) {
			return "", nil, nil, fmt.Errorf("unknown column %q on table %q", join.LeftColumn, join.LeftTable)
		}
		if !validIdentifier(schema, join.Table, join.RightColumn) {
			return "", nil, nil, fmt.Errorf("unknown column %q on table %q", join.RightColumn, join.Table)
		}
		joinClauses = append(joinClauses, fmt.Sprintf("%s JOIN %q ON %q.%q = %q.%q",
			joinType, join.Table, join.LeftTable, join.LeftColumn, join.Table, join.RightColumn))
		joinedTables[join.Table] = true
	}

	hasGroupBy := len(req.GroupBy) > 0

	var selectClauses []string
	var colAliases []string
	seenAlias := map[string]bool{}
	for _, col := range req.Select {
		if !joinedTables[col.Table] {
			return "", nil, nil, fmt.Errorf("select references %q, which isn't the base table or a joined table", col.Table)
		}
		if !validIdentifier(schema, col.Table, col.Column) {
			return "", nil, nil, fmt.Errorf("unknown column %q on table %q", col.Column, col.Table)
		}
		aggregate := strings.ToUpper(strings.TrimSpace(col.Aggregate))
		if aggregate != "" && !aggregateFunctions[aggregate] {
			return "", nil, nil, fmt.Errorf("unsupported aggregate %q (use COUNT, SUM, AVG, MIN, or MAX)", col.Aggregate)
		}
		if aggregate != "" && !hasGroupBy {
			return "", nil, nil, fmt.Errorf("aggregate on %q.%q requires at least one Group By column", col.Table, col.Column)
		}

		displayName := col.Column
		if col.Alias != "" {
			if !identifierPattern.MatchString(col.Alias) {
				return "", nil, nil, fmt.Errorf("invalid alias %q", col.Alias)
			}
			displayName = col.Alias
		} else if aggregate != "" {
			displayName = strings.ToLower(aggregate) + "_" + col.Column
		} else if len(req.Joins) > 0 {
			// Multiple tables in play and no alias given — qualify the
			// display name so two same-named columns from different
			// tables (e.g. two "id" columns) don't collide in the result.
			displayName = col.Table + "." + col.Column
		}
		if seenAlias[displayName] {
			return "", nil, nil, fmt.Errorf("duplicate result column %q — give one of them an alias", displayName)
		}
		seenAlias[displayName] = true

		sqlAlias := strings.ReplaceAll(displayName, ".", "__")
		columnRef := fmt.Sprintf("%q.%q", col.Table, col.Column)
		if aggregate != "" {
			columnRef = fmt.Sprintf("%s(%s)", aggregate, columnRef)
		}
		selectClauses = append(selectClauses, fmt.Sprintf("%s AS %q", columnRef, sqlAlias))
		colAliases = append(colAliases, displayName)
	}

	// A correlated subquery (exists/not_exists, or in_subquery/not_in_subquery
	// used correlated) may reference any table reachable from here — passed
	// down as ITS outerTables, accumulated so a subquery-of-a-subquery can
	// still correlate all the way up if it needs to.
	subqueryOuterTables := map[string]bool{}
	for t := range outerTables {
		subqueryOuterTables[t] = true
	}
	for t := range joinedTables {
		subqueryOuterTables[t] = true
	}

	var whereClauses []string
	var args []interface{}
	for i, cond := range req.Where {
		sqlOperator, ok := whereOperators[cond.Operator]
		if !ok {
			return "", nil, nil, fmt.Errorf("unsupported operator %q", cond.Operator)
		}

		var fragment string
		switch cond.Operator {
		case "exists", "not_exists":
			if cond.Subquery == nil {
				return "", nil, nil, fmt.Errorf("%q requires a subquery", cond.Operator)
			}
			subSQL, subArgs, _, err := buildQuerySQL(*cond.Subquery, schema, depth+1, subqueryOuterTables)
			if err != nil {
				return "", nil, nil, fmt.Errorf("subquery: %w", err)
			}
			fragment = fmt.Sprintf("%s (%s)", sqlOperator, subSQL)
			args = append(args, subArgs...)

		default:
			if !reachable(cond.Table) {
				return "", nil, nil, fmt.Errorf("where clause references %q, which isn't reachable from this query", cond.Table)
			}
			if !validIdentifier(schema, cond.Table, cond.Column) {
				return "", nil, nil, fmt.Errorf("unknown column %q on table %q", cond.Column, cond.Table)
			}
			columnRef := fmt.Sprintf("%q.%q", cond.Table, cond.Column)

			switch cond.Operator {
			case "is_null", "is_not_null":
				fragment = fmt.Sprintf("%s %s", columnRef, sqlOperator)
			case "in_subquery", "not_in_subquery":
				if cond.Subquery == nil {
					return "", nil, nil, fmt.Errorf("%q requires a subquery", cond.Operator)
				}
				subSQL, subArgs, subCols, err := buildQuerySQL(*cond.Subquery, schema, depth+1, subqueryOuterTables)
				if err != nil {
					return "", nil, nil, fmt.Errorf("subquery: %w", err)
				}
				if len(subCols) != 1 {
					return "", nil, nil, fmt.Errorf("a subquery used with %s must select exactly one column, got %d", cond.Operator, len(subCols))
				}
				fragment = fmt.Sprintf("%s %s (%s)", columnRef, sqlOperator, subSQL)
				args = append(args, subArgs...)
			case "contains", "starts_with", "ends_with":
				if cond.ValueColumn != nil {
					return "", nil, nil, fmt.Errorf("%q compares against a value, not a column", cond.Operator)
				}
				wrapped := cond.Value
				if cond.Operator == "contains" {
					wrapped = "%" + cond.Value + "%"
				} else if cond.Operator == "starts_with" {
					wrapped = cond.Value + "%"
				} else {
					wrapped = "%" + cond.Value
				}
				fragment = fmt.Sprintf("%s %s ?", columnRef, sqlOperator)
				args = append(args, wrapped)
			default: // =, !=, <, <=, >, >=
				if cond.ValueColumn != nil {
					vc := cond.ValueColumn
					if !reachable(vc.Table) {
						return "", nil, nil, fmt.Errorf("where clause references %q, which isn't reachable from this query", vc.Table)
					}
					if !validIdentifier(schema, vc.Table, vc.Column) {
						return "", nil, nil, fmt.Errorf("unknown column %q on table %q", vc.Column, vc.Table)
					}
					fragment = fmt.Sprintf("%s %s %q.%q", columnRef, sqlOperator, vc.Table, vc.Column)
				} else {
					fragment = fmt.Sprintf("%s %s ?", columnRef, sqlOperator)
					args = append(args, cond.Value)
				}
			}
		}

		if cond.Negate {
			fragment = "NOT (" + fragment + ")"
		}

		if i == 0 {
			whereClauses = append(whereClauses, fragment)
			continue
		}
		combinator := strings.ToUpper(strings.TrimSpace(cond.Combinator))
		if combinator == "" {
			combinator = "AND"
		}
		if combinator != "AND" && combinator != "OR" {
			return "", nil, nil, fmt.Errorf("unsupported combinator %q (use AND or OR)", cond.Combinator)
		}
		whereClauses = append(whereClauses, combinator, fragment)
	}

	var groupByClauses []string
	for _, col := range req.GroupBy {
		if !joinedTables[col.Table] {
			return "", nil, nil, fmt.Errorf("group by references %q, which isn't the base table or a joined table", col.Table)
		}
		if !validIdentifier(schema, col.Table, col.Column) {
			return "", nil, nil, fmt.Errorf("unknown column %q on table %q", col.Column, col.Table)
		}
		groupByClauses = append(groupByClauses, fmt.Sprintf("%q.%q", col.Table, col.Column))
	}

	// A subquery (depth > 0) feeds an IN (...) filter, not something shown to
	// the user — silently truncating it at the normal page-size default would
	// drop candidate rows from the set and change which outer rows match,
	// which is a correctness bug, not a display nicety. Only cap an
	// explicitly oversized limit there; leave it unbounded otherwise. The
	// top-level request (depth == 0) keeps the original default-100 paging
	// behavior.
	limit := req.Limit
	if depth == 0 && limit <= 0 {
		limit = databasePageSize
	}
	if limit > maxDatabasePageSize {
		limit = maxDatabasePageSize
	}

	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %q", strings.Join(selectClauses, ", "), req.From)
	if len(joinClauses) > 0 {
		fmt.Fprintf(&b, " %s", strings.Join(joinClauses, " "))
	}
	if len(whereClauses) > 0 {
		// whereClauses already interleaves each condition's own combinator
		// (AND/OR) between fragments — see the loop above — so this just
		// joins with plain spaces, not a fixed " AND ".
		fmt.Fprintf(&b, " WHERE %s", strings.Join(whereClauses, " "))
	}
	if len(groupByClauses) > 0 {
		fmt.Fprintf(&b, " GROUP BY %s", strings.Join(groupByClauses, ", "))
	}
	if limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", limit)
	}

	return b.String(), args, colAliases, nil
}

// parsePositiveQueryInt reads a non-negative integer query parameter, falling
// back to fallbackValue when absent or malformed. maximum of 0 means no cap.
func parsePositiveQueryInt(r *http.Request, key string, fallbackValue, maximum int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallbackValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallbackValue
	}
	if maximum > 0 && value > maximum {
		return maximum
	}
	return value
}
