// Package repository holds every SQL statement in the service. Keeping SQL in
// exactly one layer means the service layer can be unit-tested against
// interfaces, and a query change never ripples outside this package.
package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgUniqueViolation is Postgres error code 23505. Detecting it lets us turn a
// race-lost INSERT into a clean 409 instead of a 500 — checking "does it exist"
// before inserting would still leave a window for a concurrent request.
const pgUniqueViolation = "23505"

// isUniqueViolation reports whether err is a unique-constraint failure on the
// named constraint. Passing the constraint name matters: a table can carry
// several unique indexes and they mean different things to the caller.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgUniqueViolation && (constraint == "" || pgErr.ConstraintName == constraint)
}

// Querier is the subset of *sql.DB and *sql.Tx that repositories use, so the
// same repository code runs inside or outside a transaction.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

var _ Querier = (*sql.DB)(nil)
var _ Querier = (*sql.Tx)(nil)
