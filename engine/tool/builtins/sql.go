package builtins

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/spawn08/chronos/engine/tool"
)

// NewSQLTool creates a tool that executes SQL queries against a database.
// db is a *sql.DB connection. allowedOperations restricts which SQL operations
// are permitted (e.g., "SELECT", "INSERT"). An empty list allows only SELECT.
func NewSQLTool(db *sql.DB, allowedOperations []string) *tool.Definition {
	if len(allowedOperations) == 0 {
		allowedOperations = []string{"SELECT"}
	}
	allowed := make(map[string]bool, len(allowedOperations))
	for _, op := range allowedOperations {
		allowed[strings.ToUpper(op)] = true
	}

	return &tool.Definition{
		Name:        "sql_query",
		Description: "Execute a SQL query against the database and return results as rows.",
		Permission:  tool.PermRequireApproval,
		RequiresConfirmation: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The SQL query to execute",
				},
				"params": map[string]any{
					"type":        "array",
					"description": "Positional parameters for the query (for parameterized queries)",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return nil, fmt.Errorf("sql_query: 'query' argument is required")
			}

			// Validate operation
			op := strings.ToUpper(strings.TrimSpace(query))
			opAllowed := false
			for a := range allowed {
				if strings.HasPrefix(op, a) {
					opAllowed = true
					break
				}
			}
			if !opAllowed {
				return nil, fmt.Errorf("sql_query: operation not allowed; permitted: %v", allowedOperations)
			}

			// Parse params
			var queryParams []any
			if p, ok := args["params"].([]any); ok {
				queryParams = p
			}

			// Determine if this is a query (returns rows) or exec (returns affected rows)
			upperQuery := strings.ToUpper(strings.TrimSpace(query))
			if strings.HasPrefix(upperQuery, "SELECT") || strings.HasPrefix(upperQuery, "WITH") {
				return executeQuery(ctx, db, query, queryParams)
			}
			return executeExec(ctx, db, query, queryParams)
		},
	}
}

func executeQuery(ctx context.Context, db *sql.DB, query string, params []any) (any, error) {
	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("sql_query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("sql_query: getting columns: %w", err)
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("sql_query: scanning row: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[col] = v
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sql_query: iterating rows: %w", err)
	}

	return map[string]any{
		"columns": cols,
		"rows":    results,
		"count":   len(results),
	}, nil
}

func executeExec(ctx context.Context, db *sql.DB, query string, params []any) (any, error) {
	result, err := db.ExecContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("sql_query: %w", err)
	}

	affected, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()

	return map[string]any{
		"rows_affected":  affected,
		"last_insert_id": lastID,
	}, nil
}
