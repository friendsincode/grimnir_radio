/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

var searchNormalizer = strings.NewReplacer(
	" ", "",
	".", "",
	"-", "",
	"_", "",
	"'", "",
	"\"", "",
	"/", "",
	"\\", "",
	"(", "",
	")", "",
	"[", "",
	"]", "",
	",", "",
	";", "",
	":", "",
)

func normalizeSearchText(s string) string {
	return searchNormalizer.Replace(strings.ToLower(strings.TrimSpace(s)))
}

func normalizedSQLExpr(col string) string {
	// Keep this to SQL functions shared by postgres/mysql/sqlite.
	return fmt.Sprintf(
		`REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(LOWER(%s), ' ', ''), '.', ''), '-', ''), '_', ''), '''', ''), '"', ''), '/', ''), '\\', ''), '(', ''), ')', ''), '[', ''), ']', ''), ',', ''), ';', '')`,
		col,
	)
}

func applyLooseMediaSearch(db *gorm.DB, query string) *gorm.DB {
	q := strings.TrimSpace(query)
	if q == "" {
		return db
	}

	pattern := "%" + strings.ToLower(q) + "%"
	norm := "%" + normalizeSearchText(q) + "%"

	// Search across title, artist, album, genre, and original filename.
	// Both plain LOWER() LIKE and punctuation-normalized variants are
	// checked so that e.g. "dont" matches "Don't".
	where := fmt.Sprintf(
		`LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ? OR LOWER(genre) LIKE ? OR LOWER(original_filename) LIKE ? OR %s LIKE ? OR %s LIKE ? OR %s LIKE ?`,
		normalizedSQLExpr("title"),
		normalizedSQLExpr("artist"),
		normalizedSQLExpr("album"),
	)

	return db.Where(where, pattern, pattern, pattern, pattern, pattern, norm, norm, norm)
}
