package sqlitedb_test

// schema_lint_test.go — static analysis guards for huginn-sqlite-schema.sql.
//
// Rule: Every column referenced by a CREATE INDEX statement in schema.sql
// must exist in the CREATE TABLE statement for that table within the same
// file. If it does not, the index belongs in a migration (next to the ALTER
// TABLE that adds the column), NOT in schema.sql.
//
// Violations of this rule caused the production outage on 2026-03-17:
// CREATE INDEX ON messages(parent_message_id) failed on upgraded databases
// because the column was added by a migration, not in the original CREATE
// TABLE. ApplySchema aborted entirely, leaving sqlDB nil and returning 503
// for all SQLite-backed endpoints.

import (
	"bufio"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

//go:embed schema/huginn-sqlite-schema.sql
var lintSchemaSQL string

// TestSchemaLint_NoIndexOnMigrationOnlyColumns parses schema.sql and verifies
// that every column referenced in a CREATE INDEX statement also appears in the
// corresponding CREATE TABLE block.
//
// If this test fails:
//  1. Move the offending CREATE INDEX to the migration that adds the column.
//  2. Ensure the migration uses IF NOT EXISTS so it is idempotent on fresh DBs.
func TestSchemaLint_NoIndexOnMigrationOnlyColumns(t *testing.T) {
	tableColumns := parseTableColumns(lintSchemaSQL)
	indexes := parseIndexes(lintSchemaSQL)

	for _, idx := range indexes {
		cols, ok := tableColumns[idx.table]
		if !ok {
			// Table not defined in schema.sql — skip (external table or typo
			// caught by other tests).
			continue
		}
		for _, col := range idx.columns {
			if !cols[col] {
				t.Errorf(
					"schema.sql: index %q references column %q on table %q, "+
						"but that column is not in the CREATE TABLE statement.\n\n"+
						"Fix: move this CREATE INDEX into the migration that adds %q via ALTER TABLE.\n"+
						"Use CREATE INDEX IF NOT EXISTS so it is idempotent on fresh installs.",
					idx.name, col, idx.table, col,
				)
			}
		}
	}
}

// TestSchemaLint_ApplySchemaOnFreshDB verifies that ApplySchema succeeds on a
// completely empty database. All columns referenced by CREATE INDEX statements
// must exist in the corresponding CREATE TABLE — migrations are squashed into
// the base schema, so no ALTER TABLE additions are expected.
func TestSchemaLint_ApplySchemaOnFreshDB(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.ApplySchema(); err != nil {
		t.Fatalf(
			"ApplySchema failed on fresh database: %v\n\n"+
				"schema.sql contains a CREATE INDEX that references a column "+
				"not present in the CREATE TABLE. Add the column to the CREATE TABLE.",
			err,
		)
	}
}

// ── parser ────────────────────────────────────────────────────────────────────

type indexDef struct {
	name    string
	table   string
	columns []string
}

var (
	reCreateIndex = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)\s+ON\s+(\w+)\s*\(([^)]+)\)`)
	reCreateTable = regexp.MustCompile(`(?i)CREATE\s+(?:VIRTUAL\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	reColumnLine  = regexp.MustCompile(`^\s*(\w+)\s+(?:TEXT|INTEGER|REAL|BLOB|NUMERIC)`)
)

// parseTableColumns returns map[tableName]set[columnName] from CREATE TABLE blocks.
func parseTableColumns(sql string) map[string]map[string]bool {
	result := map[string]map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(sql))

	var currentTable string
	var depth int

	for scanner.Scan() {
		line := scanner.Text()

		if currentTable == "" {
			if m := reCreateTable.FindStringSubmatch(line); m != nil {
				currentTable = strings.ToLower(m[1])
				result[currentTable] = map[string]bool{}
				depth = strings.Count(line, "(") - strings.Count(line, ")")
			}
			continue
		}

		// Inside a CREATE TABLE block — count parens and collect column names.
		depth += strings.Count(line, "(") - strings.Count(line, ")")

		if m := reColumnLine.FindStringSubmatch(line); m != nil {
			colName := strings.ToLower(m[1])
			// Skip SQL keywords that start lines (PRIMARY, FOREIGN, UNIQUE, CHECK, CONSTRAINT).
			skip := map[string]bool{
				"primary": true, "foreign": true, "unique": true,
				"check": true, "constraint": true,
			}
			if !skip[colName] {
				result[currentTable][colName] = true
			}
		}

		if depth <= 0 {
			currentTable = ""
			depth = 0
		}
	}
	return result
}

// parseIndexes extracts CREATE INDEX definitions from the SQL.
func parseIndexes(sql string) []indexDef {
	var out []indexDef
	// Strip WHERE clauses before matching columns.
	stripped := regexp.MustCompile(`(?i)\bWHERE\b.*`).ReplaceAllString(sql, "")
	matches := reCreateIndex.FindAllStringSubmatch(stripped, -1)
	for _, m := range matches {
		name := strings.ToLower(m[1])
		table := strings.ToLower(m[2])
		rawCols := m[3]
		var cols []string
		for _, c := range strings.Split(rawCols, ",") {
			// Strip sort direction (ASC/DESC) and functions.
			c = strings.TrimSpace(c)
			c = regexp.MustCompile(`(?i)\s+(ASC|DESC)$`).ReplaceAllString(c, "")
			c = strings.ToLower(strings.TrimSpace(c))
			if c != "" {
				cols = append(cols, c)
			}
		}
		out = append(out, indexDef{name: name, table: table, columns: cols})
	}
	return out
}

// TestSchemaLint_ParsersWork is a sanity check for the parser logic.
func TestSchemaLint_ParsersWork(t *testing.T) {
	sample := `
CREATE TABLE IF NOT EXISTS widgets (
    id   TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_widgets_name ON widgets (name);
CREATE INDEX IF NOT EXISTS idx_widgets_missing ON widgets (nonexistent_col);
`
	cols := parseTableColumns(sample)
	widgetCols, ok := cols["widgets"]
	if !ok {
		t.Fatal("widgets table not parsed")
	}
	if !widgetCols["name"] {
		t.Error("expected 'name' column in widgets")
	}

	indexes := parseIndexes(sample)
	if len(indexes) != 2 {
		t.Fatalf("expected 2 indexes, got %d", len(indexes))
	}

	// Simulate the lint check.
	violations := 0
	for _, idx := range indexes {
		for _, col := range idx.columns {
			if !widgetCols[col] {
				violations++
				_ = fmt.Sprintf("violation: %s.%s", idx.table, col)
			}
		}
	}
	if violations != 1 {
		t.Errorf("expected 1 violation (nonexistent_col), got %d", violations)
	}
}
