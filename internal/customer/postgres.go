package customer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PostgresRepository stores customers and screenings in PostgreSQL.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a repository on top of an open database.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// date_of_birth::text returns the date as "YYYY-MM-DD".
const customerColumns = `id, full_name, date_of_birth::text, country, status, created_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCustomer(s rowScanner) (Customer, error) {
	var c Customer
	err := s.Scan(&c.ID, &c.FullName, &c.DateOfBirth, &c.Country, &c.Status, &c.CreatedAt)
	return c, err
}

func (r *PostgresRepository) Create(ctx context.Context, in CreateInput) (Customer, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO customers (full_name, date_of_birth, country)
		VALUES ($1, $2, $3)
		RETURNING `+customerColumns,
		in.FullName, in.DateOfBirth, in.Country,
	)
	c, err := scanCustomer(row)
	if err != nil {
		return Customer{}, fmt.Errorf("insert customer: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (Customer, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = $1`, id)
	c, err := scanCustomer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Customer{}, ErrNotFound
	}
	if err != nil {
		return Customer{}, fmt.Errorf("select customer: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) List(ctx context.Context, limit, offset int) ([]Customer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+customerColumns+`
		FROM customers
		ORDER BY created_at DESC, id
		LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("select customers: %w", err)
	}
	defer rows.Close()

	cs := []Customer{} // empty list encodes as [] instead of null
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan customer: %w", err)
		}
		cs = append(cs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate customers: %w", err)
	}
	return cs, nil
}

// SaveScreening inserts the screening and updates the customer status in a
// single transaction, so we never have a status without the screening that
// explains it (or the other way around).
func (r *PostgresRepository) SaveScreening(ctx context.Context, s Screening, newStatus Status) (Screening, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Screening{}, fmt.Errorf("begin tx: %w", err)
	}
	// Rollback does nothing if Commit already succeeded.
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `UPDATE customers SET status = $1 WHERE id = $2`, newStatus, s.CustomerID)
	if err != nil {
		return Screening{}, fmt.Errorf("update customer status: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Screening{}, fmt.Errorf("rows affected: %w", err)
	} else if n == 0 {
		return Screening{}, ErrNotFound
	}

	matched := sql.NullString{String: s.MatchedName, Valid: s.MatchedName != ""}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO screenings (customer_id, result, matched_name, score, list_version)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		s.CustomerID, s.Result, matched, s.Score, s.ListVersion,
	).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return Screening{}, fmt.Errorf("insert screening: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Screening{}, fmt.Errorf("commit: %w", err)
	}
	return s, nil
}

func (r *PostgresRepository) ListScreenings(ctx context.Context, customerID string) ([]Screening, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, customer_id, result, matched_name, score, list_version, created_at
		FROM screenings
		WHERE customer_id = $1
		ORDER BY created_at DESC, id`,
		customerID,
	)
	if err != nil {
		return nil, fmt.Errorf("select screenings: %w", err)
	}
	defer rows.Close()

	list := []Screening{}
	for rows.Next() {
		var s Screening
		var matched sql.NullString
		if err := rows.Scan(&s.ID, &s.CustomerID, &s.Result, &matched, &s.Score, &s.ListVersion, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan screening: %w", err)
		}
		s.MatchedName = matched.String
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate screenings: %w", err)
	}
	return list, nil
}
