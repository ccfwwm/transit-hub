package upstream

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrateCredentialJSON upgrades an existing JSON column under row locks.
// Identifiers and the source predicate come from the calling repository.
func MigrateCredentialJSON(ctx context.Context, db *pgxpool.Pool, relation, field, predicate string, transform func([]byte) ([]byte, error)) error {
	table, column := pgx.Identifier{relation}.Sanitize(), pgx.Identifier{field}.Sanitize()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, fmt.Sprintf("SELECT ctid::text,%s FROM %s WHERE %s FOR UPDATE", column, table, predicate))
	if err != nil {
		return err
	}
	type row struct {
		location string
		raw      []byte
	}
	var pending []row
	for rows.Next() {
		var entry row
		if err = rows.Scan(&entry.location, &entry.raw); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, entry := range pending {
		sealed, err := transform(entry.raw)
		if err != nil {
			return fmt.Errorf("protect %s.%s: %w", relation, field, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s SET %s=$2::jsonb WHERE ctid=$1::text::tid", table, column), entry.location, string(sealed)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
