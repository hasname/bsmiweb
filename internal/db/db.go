package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB     *sql.DB
	Driver string
}

const Version = 3

func Open(driver, dsn string) (*Store, error) {
	switch driver {
	case "mysql":
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid MySQL DSN")
		}
		cfg.ParseTime, cfg.MultiStatements, cfg.Loc = true, true, time.UTC
		if cfg.Params == nil {
			cfg.Params = map[string]string{}
		}
		cfg.Params["charset"] = "utf8mb4"
		cfg.Timeout = 10 * time.Second
		dsn = cfg.FormatDSN()
	case "sqlite":
		if dsn == "" {
			dsn = ":memory:"
		}
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)"
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		conn.SetMaxOpenConns(1)
	} else {
		conn.SetMaxOpenConns(10)
		conn.SetConnMaxLifetime(3 * time.Minute)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return &Store{DB: conn, Driver: driver}, nil
}

func (s *Store) CheckVersion(ctx context.Context) error {
	var version int
	var dirty bool
	if err := s.DB.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		return fmt.Errorf("database is not migrated; run bsmiweb migrate: %w", err)
	}
	if dirty || version != Version {
		return fmt.Errorf("database migration version=%d dirty=%t; run bsmiweb migrate and resolve any migration failure", version, dirty)
	}
	return nil
}
