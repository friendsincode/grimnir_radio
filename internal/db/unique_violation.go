/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). Create endpoints use it to turn a duplicate key
// (a duplicate station name, a re-used email) into a clean domain error instead
// of a generic 500, and it closes the check-then-create race: the loser gets a
// clean rejection rather than an unhandled driver error.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
