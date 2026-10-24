package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/mysql/*.sql migrations/sqlite/*.sql
var migrations embed.FS

var legacyNames = []string{"0001_baseline", "0002_rename_to_snake_case", "0003_remove_empty_registrations"}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Migrate adopts only verified Prisma schemas. It never repairs dirty migrations
// or marks pending SQL as applied. The original Prisma history remains intact.
func (s *Store) Migrate(ctx context.Context) error {
	var q queryer = s.DB
	var conn *sql.Conn
	if s.Driver == "mysql" {
		var err error
		conn, err = s.DB.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		var locked int
		if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT(DATABASE(), ':bsmiweb:migrate'), 30)").Scan(&locked); err != nil {
			return err
		}
		if locked != 1 {
			return fmt.Errorf("could not acquire migration lock")
		}
		defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(CONCAT(DATABASE(), ':bsmiweb:migrate'))")
		q = conn
	}
	tables, err := s.tables(ctx, q)
	if err != nil {
		return err
	}
	adopt := 0
	if tables["schema_migrations"] {
		var version int
		var dirty bool
		err := q.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if dirty {
				return fmt.Errorf("migration %d is dirty; inspect and repair it manually before retrying", version)
			}
			if version < 1 || version > Version {
				return fmt.Errorf("unsupported migration version %d", version)
			}
			return s.runMigrations(ctx, conn, 0)
		}
	}
	if tables["_prisma_migrations"] {
		if s.Driver != "mysql" {
			return fmt.Errorf("unexpected Prisma migration history in SQLite database")
		}
		adopt, err = prismaVersion(ctx, q)
		if err != nil {
			return err
		}
	}
	hasData := tables["registration"] || tables["Registration"] || tables["certificate"] || tables["Certificate"] || tables["authorization"] || tables["Authorization"]
	if hasData {
		if s.Driver == "sqlite" {
			adopt = 2
		}
		if adopt == 0 {
			return fmt.Errorf("existing tables have no completed Prisma baseline; refusing to adopt an unknown schema")
		}
		if err := s.validateLegacy(ctx, q, tables, adopt); err != nil {
			return err
		}
	} else if adopt != 0 {
		return fmt.Errorf("Prisma history exists but application tables are missing")
	}
	return s.runMigrations(ctx, conn, adopt)
}

func (s *Store) runMigrations(ctx context.Context, conn *sql.Conn, adopt int) error {
	src, err := iofs.New(migrations, "migrations/"+s.Driver)
	if err != nil {
		return err
	}
	defer src.Close()
	var driver database.Driver
	if s.Driver == "mysql" {
		driver, err = migratemysql.WithConnection(ctx, conn, &migratemysql.Config{})
	} else {
		driver, err = migratesqlite.WithInstance(s.DB, &migratesqlite.Config{})
	}
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, s.Driver, driver)
	if err != nil {
		return err
	}
	// Do not close m: SQLite's migration driver owns the application's DB.
	if adopt > 0 {
		if err := m.Force(adopt); err != nil {
			return err
		}
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (s *Store) tables(ctx context.Context, q queryer) (map[string]bool, error) {
	query := "SELECT name FROM sqlite_master WHERE type='table'"
	if s.Driver == "mysql" {
		query = "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()"
	}
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables[name] = true
	}
	return tables, rows.Err()
}

func prismaVersion(ctx context.Context, q queryer) (int, error) {
	rows, err := q.QueryContext(ctx, "SELECT migration_name, checksum, finished_at IS NOT NULL, rolled_back_at IS NOT NULL FROM _prisma_migrations ORDER BY started_at")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var name, checksum string
		var finished, rolledBack bool
		if err := rows.Scan(&name, &checksum, &finished, &rolledBack); err != nil {
			return 0, err
		}
		if rolledBack {
			continue
		}
		if !finished {
			return 0, fmt.Errorf("unfinished Prisma migration %s; resolve it with Prisma first", name)
		}
		index := -1
		for i, known := range legacyNames {
			if known == name {
				index = i
				break
			}
		}
		if index == -1 {
			return 0, fmt.Errorf("unknown Prisma migration %s", name)
		}
		data, err := migrations.ReadFile(fmt.Sprintf("migrations/mysql/%06d_%s.up.sql", index+1, name[5:]))
		if err != nil {
			return 0, err
		}
		if checksum != fmt.Sprintf("%x", sha256.Sum256(data)) {
			return 0, fmt.Errorf("Prisma migration checksum mismatch: %s", name)
		}
		if applied[name] {
			return 0, fmt.Errorf("duplicate completed Prisma migration %s", name)
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	version := 0
	for i, name := range legacyNames {
		if applied[name] {
			if i != version {
				return 0, fmt.Errorf("noncontiguous Prisma migration history")
			}
			version++
		}
	}
	return version, nil
}

var tableColumns = map[string][]string{
	"registration":  {"id", "tax_id", "applicant", "contact_addr", "company_addr", "phone", "note", "created_at", "updated_at"},
	"certificate":   {"id", "registration_id", "valid_date", "status", "product_name", "sold_as", "main_model", "series_models", "issuer", "created_at", "updated_at"},
	"authorization": {"id", "certificate_id", "authorizer_name", "main_model", "authorizee_tax_id", "authorizee_name", "authorizee_addr", "authorizee_phone", "valid_date", "created_at", "updated_at"},
}

func legacyColumn(name string, version int) string {
	if version != 1 {
		return name
	}
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func (s *Store) validateLegacy(ctx context.Context, q queryer, tables map[string]bool, version int) error {
	for table, columns := range tableColumns {
		name, other := table, strings.ToUpper(table[:1])+table[1:]
		if version == 1 {
			name, other = other, name
		}
		if !tables[name] || tables[other] {
			return fmt.Errorf("schema does not match Prisma version %d: table %s", version, name)
		}
		var query string
		var args []any
		if s.Driver == "mysql" {
			query = "SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?"
			args = []any{name}
		} else {
			query = "SELECT name, type, CASE WHEN [notnull]=1 OR pk=1 THEN 'NO' ELSE 'YES' END, CASE WHEN pk=1 THEN 'PRI' ELSE '' END FROM pragma_table_info(?)"
			args = []any{name}
		}
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		expectedTypes := map[string]string{}
		for _, column := range columns {
			typ := "text"
			if s.Driver == "mysql" {
				typ = "varchar"
			}
			if column == "created_at" || column == "updated_at" {
				typ = "datetime"
			}
			switch column {
			case "contact_addr", "company_addr", "note", "product_name", "sold_as", "series_models", "authorizee_addr":
				typ = "text"
			}
			expectedTypes[legacyColumn(column, version)] = typ
		}
		for rows.Next() {
			var col, typ, nullable, key string
			if err := rows.Scan(&col, &typ, &nullable, &key); err != nil {
				rows.Close()
				return err
			}
			typ = strings.ToLower(typ)
			if nullable != "NO" || (col == "id" && key != "PRI") || (col != "id" && key == "PRI") || typ != expectedTypes[col] {
				rows.Close()
				return fmt.Errorf("unexpected definition of %s.%s", name, col)
			}
			seen[col] = true
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return rowsErr
		}
		if len(seen) != len(columns) {
			return fmt.Errorf("unexpected columns in %s", name)
		}
		for _, column := range columns {
			if !seen[legacyColumn(column, version)] {
				return fmt.Errorf("missing column %s.%s", name, column)
			}
		}
	}
	return s.validateConstraints(ctx, q, version)
}

func (s *Store) validateConstraints(ctx context.Context, q queryer, version int) error {
	tableName := func(name string) string {
		if version == 1 {
			return strings.ToUpper(name[:1]) + name[1:]
		}
		return name
	}
	for _, index := range []struct{ table, column string }{{"certificate", "registration_id"}, {"authorization", "certificate_id"}, {"authorization", "authorizee_tax_id"}} {
		table, column := tableName(index.table), legacyColumn(index.column, version)
		name := table + "_" + column + "_idx"
		query := "SELECT COUNT(*) FROM pragma_index_info(?) WHERE name=? AND seqno=0"
		args := []any{name, column}
		if s.Driver == "mysql" {
			query = "SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=? AND COLUMN_NAME=? AND SEQ_IN_INDEX=1 AND NON_UNIQUE=1"
			args = []any{table, name, column}
		}
		var count int
		if err := q.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("missing index %s", name)
		}
	}
	query := "SELECT COUNT(*) FROM pragma_foreign_key_list('certificate') WHERE [table]='registration' AND [from]='registration_id' AND [to]='id' AND on_update='CASCADE' AND on_delete='RESTRICT'"
	var args []any
	if s.Driver == "mysql" {
		query = "SELECT COUNT(*) FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME WHERE k.CONSTRAINT_SCHEMA=DATABASE() AND k.TABLE_NAME=? AND k.COLUMN_NAME=? AND k.REFERENCED_TABLE_NAME=? AND k.REFERENCED_COLUMN_NAME='id' AND r.UPDATE_RULE='CASCADE' AND r.DELETE_RULE='RESTRICT'"
		args = []any{tableName("certificate"), legacyColumn("registration_id", version), tableName("registration")}
	}
	var count int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("missing certificate registration foreign key")
	}
	return nil
}
