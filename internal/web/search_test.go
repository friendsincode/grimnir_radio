package web

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeSearchText(t *testing.T) {
	in := `  Don'T-Stop / [Now], "Vol.1"  `
	got := normalizeSearchText(in)
	want := "dontstopnowvol1"
	if got != want {
		t.Fatalf("normalizeSearchText(%q) = %q, want %q", in, got, want)
	}
}

func TestNormalizedSQLExpr(t *testing.T) {
	expr := normalizedSQLExpr("title")
	if !strings.Contains(expr, "LOWER(title)") {
		t.Fatalf("expected LOWER(title) in expression, got: %s", expr)
	}
	if !strings.Contains(expr, "REPLACE(") {
		t.Fatalf("expected nested REPLACE calls, got: %s", expr)
	}
}

func TestApplyLooseMediaSearch_BuildsExpectedPatternAndVars(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	tx := db.Session(&gorm.Session{DryRun: true}).Table("media_items")
	tx = applyLooseMediaSearch(tx, " Don't-Stop ")
	tx = tx.Find(&[]map[string]any{})

	sql := tx.Statement.SQL.String()
	if !strings.Contains(sql, "LOWER(title) LIKE ?") {
		t.Fatalf("expected title LIKE clause, sql=%s", sql)
	}
	if !strings.Contains(sql, "LOWER(original_filename) LIKE ?") {
		t.Fatalf("expected filename LIKE clause, sql=%s", sql)
	}

	if len(tx.Statement.Vars) != 8 {
		t.Fatalf("expected 8 vars, got %d (%v)", len(tx.Statement.Vars), tx.Statement.Vars)
	}
	if tx.Statement.Vars[0] != "%don't-stop%" {
		t.Fatalf("expected raw pattern var, got %v", tx.Statement.Vars[0])
	}
	if tx.Statement.Vars[5] != "%dontstop%" {
		t.Fatalf("expected normalized pattern var, got %v", tx.Statement.Vars[5])
	}
}

func TestApplyLooseMediaSearch_EmptyQueryNoWhere(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	tx := db.Session(&gorm.Session{DryRun: true}).Table("media_items")
	tx = applyLooseMediaSearch(tx, "   ")
	tx = tx.Find(&[]map[string]any{})

	if strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "WHERE") {
		t.Fatalf("expected no WHERE clause for empty query, sql=%s", tx.Statement.SQL.String())
	}
}
