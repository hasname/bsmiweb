package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// BSMI_TEST_MYSQL_DSN must identify a disposable test server. Tests create and
// drop uniquely named databases; they never use the application's environment.
func mysqlStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("BSMI_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("BSMI_TEST_MYSQL_DSN is not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("bsmiweb_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName = name
	s, err := Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func prismaHistory(t *testing.T, s *Store, version int) {
	t.Helper()
	exec(t, s, "CREATE TABLE _prisma_migrations(id VARCHAR(36) PRIMARY KEY, checksum VARCHAR(64) NOT NULL, migration_name VARCHAR(255) NOT NULL, started_at DATETIME(3) NOT NULL, finished_at DATETIME(3), rolled_back_at DATETIME(3))")
	for i := 1; i <= version; i++ {
		name := legacyNames[i-1]
		data, err := migrations.ReadFile(fmt.Sprintf("migrations/mysql/%06d_%s.up.sql", i, name[5:]))
		if err != nil {
			t.Fatal(err)
		}
		exec(t, s, string(data))
		exec(t, s, "INSERT INTO _prisma_migrations VALUES(?,?,?,CURRENT_TIMESTAMP(3),CURRENT_TIMESTAMP(3),NULL)", fmt.Sprint(i), fmt.Sprintf("%x", sha256.Sum256(data)), name)
	}
}

func TestMySQLMigrations(t *testing.T) {
	ctx := context.Background()
	t.Run("fresh and repeat", func(t *testing.T) {
		s := mysqlStore(t)
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckVersion(ctx); err != nil {
			t.Fatal(err)
		}
		testImport(t, s)
	})
	for _, version := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("Prisma version %d", version), func(t *testing.T) {
			s := mysqlStore(t)
			prismaHistory(t, s, version)
			if version == 1 {
				exec(t, s, "INSERT INTO Registration VALUES('R45879','82781974','kept','','','','',CURRENT_TIMESTAMP(3),CURRENT_TIMESTAMP(3))")
				exec(t, s, "INSERT INTO Certificate VALUES('KEEP','R45879','','','','','','','',CURRENT_TIMESTAMP(3),CURRENT_TIMESTAMP(3))")
			} else {
				if err := s.SaveRegistration(ctx, sample()); err != nil {
					t.Fatal(err)
				}
			}
			if version == 2 {
				if err := s.SaveRegistration(ctx, &Registration{ID: "R00000"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			r, err := s.Registration(ctx, "R45879")
			if err != nil || r == nil || len(r.Certificates) != 1 {
				t.Fatalf("data lost: %+v %v", r, err)
			}
			var history int
			s.DB.QueryRow("SELECT COUNT(*) FROM _prisma_migrations").Scan(&history)
			if history != version {
				t.Fatal("Prisma history changed")
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			after, _ := s.Registration(ctx, r.ID)
			if !reflect.DeepEqual(r, after) {
				t.Fatal("repeat changed data")
			}
			if version == 2 {
				empty, _ := s.Registration(ctx, "R00000")
				if empty != nil {
					t.Fatal("pending cleanup was skipped")
				}
			}
		})
	}
	for _, tc := range []struct{ name, change string }{
		{"unfinished", "UPDATE _prisma_migrations SET finished_at=NULL WHERE id='2'"},
		{"checksum mismatch", "UPDATE _prisma_migrations SET checksum=REPEAT('0',64) WHERE id='1'"},
		{"history gap", "DELETE FROM _prisma_migrations WHERE id='1'"},
		{"unknown migration", "UPDATE _prisma_migrations SET migration_name='9999_unknown' WHERE id='2'"},
		{"missing column", "ALTER TABLE registration DROP COLUMN phone"},
		{"wrong column type", "ALTER TABLE registration MODIFY tax_id TEXT NOT NULL"},
		{"missing index", "ALTER TABLE authorization DROP INDEX authorization_certificate_id_idx"},
		{"missing foreign key", "ALTER TABLE certificate DROP FOREIGN KEY certificate_registration_id_fkey"},
		{"no history", "DROP TABLE _prisma_migrations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mysqlStore(t)
			prismaHistory(t, s, 2)
			exec(t, s, tc.change)
			if err := s.Migrate(ctx); err == nil {
				t.Fatal("accepted invalid legacy database")
			}
			var count int
			s.DB.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations'").Scan(&count)
			if count != 0 {
				t.Fatal("invalid database was marked as migrated")
			}
		})
	}
	t.Run("rolled back retry", func(t *testing.T) {
		s := mysqlStore(t)
		prismaHistory(t, s, 2)
		exec(t, s, "INSERT INTO _prisma_migrations VALUES('retry','ignored','0003_remove_empty_registrations',CURRENT_TIMESTAMP(3),NULL,CURRENT_TIMESTAMP(3))")
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
