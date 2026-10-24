package db

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}
func exec(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func sample() *Registration {
	return &Registration{ID: "R45879", TaxID: "82781974", Applicant: "測試公司", Certificates: []Certificate{{ID: "CI450068790054", MainModel: "original", SeriesModels: "A\nB", Issuer: "BSMI"}}}
}

func TestFreshMigrationsAndRepeat(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CheckVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRegistration(ctx, sample()); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Registration(ctx, "R45879")
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Registration(ctx, "R45879")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repeat migration changed data")
	}
}

func TestAdoptSQLiteAndCleanup(t *testing.T) {
	s, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	baseline, _ := migrations.ReadFile("migrations/sqlite/000001_baseline.up.sql")
	exec(t, s, string(baseline))
	for _, r := range []*Registration{{ID: "R00000"}, {ID: "R00001", Applicant: "   "}, {ID: "R00002", Certificates: []Certificate{{ID: "KEEP"}}}, sample()} {
		if err := s.SaveRegistration(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise both timestamp formats that exist in Prisma SQLite databases.
	exec(t, s, "UPDATE registration SET created_at=?,updated_at=? WHERE id='R45879'", int64(1700000000123), "2026-10-24 12:34:56.789")
	before, _ := s.Registration(ctx, "R45879")
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Registration(ctx, "R45879")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("adoption changed vendor or certificate data")
	}
	for _, id := range []string{"R00000", "R00001"} {
		r, err := s.Registration(ctx, id)
		if err != nil || r != nil {
			t.Fatalf("empty registration survived: %s %v", id, err)
		}
	}
	r, err := s.Registration(ctx, "R00002")
	if err != nil || r == nil || len(r.Certificates) != 1 {
		t.Fatal("cleanup removed referenced registration")
	}
}

func TestRejectUnknownSchemaAndDirtyVersion(t *testing.T) {
	t.Run("unknown schema", func(t *testing.T) {
		s, err := Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer s.DB.Close()
		exec(t, s, "CREATE TABLE registration(id TEXT PRIMARY KEY)")
		if err := s.Migrate(context.Background()); err == nil {
			t.Fatal("accepted unknown schema")
		}
		var count int
		if err := s.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='schema_migrations'").Scan(&count); err != nil || count != 0 {
			t.Fatal("unknown schema was marked as migrated")
		}
	})
	t.Run("dirty", func(t *testing.T) {
		s := testStore(t)
		exec(t, s, "UPDATE schema_migrations SET dirty=1")
		if err := s.Migrate(context.Background()); err == nil || !strings.Contains(err.Error(), "dirty") {
			t.Fatal(err)
		}
		var dirty bool
		s.DB.QueryRow("SELECT dirty FROM schema_migrations").Scan(&dirty)
		if !dirty {
			t.Fatal("dirty version reset")
		}
	})
}

func TestImportPreservesLinksAndRefreshClaimsOrphans(t *testing.T) { testImport(t, testStore(t)) }
func testImport(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	r := sample()
	if err := s.SaveRegistration(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportCertificates(ctx, []Certificate{{ID: r.Certificates[0].ID, ProductName: "O'Reilly \\ 充電器", MainModel: "updated"}, {ID: "ORPHAN", ProductName: "new"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Registration(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Certificates) != 1 || got.Certificates[0].SeriesModels != "A\nB" || got.Certificates[0].MainModel != "updated" || got.Certificates[0].ProductName != "O'Reilly \\ 充電器" {
		t.Fatalf("import lost details: %+v", got)
	}
	r.Certificates = append(r.Certificates, Certificate{ID: "ORPHAN", MainModel: "linked"})
	if err := s.SaveRegistration(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err = s.Registration(ctx, r.ID)
	if err != nil || len(got.Certificates) != 2 {
		t.Fatalf("claim orphan: %+v %v", got, err)
	}
	// A failed refresh must roll back its deletion of existing certificates.
	r.Certificates = append(r.Certificates, r.Certificates[0])
	if err := s.SaveRegistration(ctx, r); err == nil {
		t.Fatal("duplicate refresh succeeded")
	}
	after, _ := s.Registration(ctx, r.ID)
	if !reflect.DeepEqual(got, after) {
		t.Fatal("failed refresh changed data")
	}
	if err := s.ReplaceAuthorizations(ctx, []Authorization{{ID: "A", AuthorizeeName: "kept"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceAuthorizations(ctx, []Authorization{{ID: "B"}, {ID: "B"}}); err == nil {
		t.Fatal("duplicate authorization succeeded")
	}
	var name string
	if err := s.DB.QueryRow("SELECT authorizee_name FROM authorization WHERE id='A'").Scan(&name); err != nil || name != "kept" {
		t.Fatal("failed replacement lost authorizations")
	}
}

func TestRefreshStats(t *testing.T) { testRefreshStats(t, testStore(t)) }
func testRefreshStats(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.SaveRegistration(ctx, sample()); err != nil {
		t.Fatal(err)
	}
	stat := func() RefreshStat {
		t.Helper()
		stats, err := s.RefreshStats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range stats {
			if v.ID == "R45879" {
				return v
			}
		}
		t.Fatalf("missing stats: %+v", stats)
		return RefreshStat{}
	}
	older, old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	exec(t, s, "UPDATE registration SET updated_at=? WHERE id='R45879'", old)
	if v := stat(); !v.SyncedAt.Equal(old) || v.Checks != 0 || v.Changes != 0 || v.CheckSeconds != 0 {
		t.Fatalf("unchecked stats: %+v", v)
	}
	if err := s.RecordCheck(ctx, "R45879", true, 86400); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCheck(ctx, "R45879", false, 3600); err != nil {
		t.Fatal(err)
	}
	if v := stat(); time.Since(v.SyncedAt) > time.Minute || v.Checks != 2 || v.Changes != 1 || v.CheckSeconds != 90000 {
		t.Fatalf("checked stats: %+v", v)
	}
	// A refresh after the last check, such as from a page view, also synchronizes.
	exec(t, s, "UPDATE registration SET checked_at=? WHERE id='R45879'", older)
	if v := stat(); !v.SyncedAt.Equal(old) {
		t.Fatalf("refresh after check: %+v", v)
	}
}

func TestImportRestoresForeignKeysOnFailure(t *testing.T) {
	s := testStore(t)
	exec(t, s, "CREATE TRIGGER reject_import BEFORE INSERT ON certificate BEGIN SELECT RAISE(FAIL, 'fail import'); END")
	if err := s.ImportCertificates(context.Background(), []Certificate{{ID: "FAIL"}}); err == nil {
		t.Fatal("expected failed import")
	}
	var enabled int
	s.DB.QueryRow("PRAGMA foreign_keys").Scan(&enabled)
	if enabled != 1 {
		t.Fatal("foreign keys were not restored")
	}
}

func TestTimestampAndMixedOrdering(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, r := range []*Registration{{ID: "R00001", Applicant: "old"}, {ID: "R00002", Applicant: "new"}} {
		if err := s.SaveRegistration(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, s, "UPDATE registration SET updated_at='2020-01-01 00:00:00' WHERE id='R00001'")
	exec(t, s, "UPDATE registration SET updated_at=? WHERE id='R00002'", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	regs, err := s.Recent(ctx)
	if err != nil || regs[0].ID != "R00002" {
		t.Fatalf("mixed timestamp ordering: %+v %v", regs, err)
	}
}

func TestLegacySQLUnchanged(t *testing.T) {
	for i, name := range legacyNames {
		original, err := os.ReadFile("../../prisma/migrations/" + name + "/migration.sql")
		if err != nil {
			t.Fatal(err)
		}
		copied, err := migrations.ReadFile("migrations/mysql/00000" + string(rune('1'+i)) + "_" + name[5:] + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		if string(original) != string(copied) {
			t.Fatalf("legacy migration changed: %s", name)
		}
	}
}
