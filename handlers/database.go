package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/entreya/nativestudio/db"
)

type DatabaseHandler struct {
	db *db.DB
}

func NewDatabaseHandler(database *db.DB) *DatabaseHandler {
	return &DatabaseHandler{db: database}
}

func (h *DatabaseHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/db/tables", h.GetTables)
	mux.HandleFunc("GET /api/projects/{id}/db/tables/{table}/data", h.GetTableData)
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
			b, ok := val.([]byte)
			if ok {
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

	writeJSON(w, map[string]interface{}{
		"columns":    columns,
		"rows":       rows,
		"total_rows": totalRows,
		"limit":      limit,
		"offset":     offset,
	})
}

const (
	databasePageSize    = 100
	maxDatabasePageSize = 500
)

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
