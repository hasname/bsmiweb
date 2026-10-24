package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

const registrationColumns = "id,tax_id,applicant,contact_addr,company_addr,phone,note,created_at,updated_at"
const certificateColumns = "id,registration_id,valid_date,status,product_name,sold_as,main_model,series_models,issuer,created_at,updated_at"
const authorizationColumns = "id,certificate_id,authorizer_name,main_model,authorizee_tax_id,authorizee_name,authorizee_addr,authorizee_phone,valid_date,created_at,updated_at"

type scanner interface{ Scan(...any) error }

func scanRegistration(row scanner) (Registration, error) {
	var v Registration
	err := row.Scan(&v.ID, &v.TaxID, &v.Applicant, &v.ContactAddr, &v.CompanyAddr, &v.Phone, &v.Note, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanCertificate(row scanner) (Certificate, error) {
	var v Certificate
	err := row.Scan(&v.ID, &v.RegistrationID, &v.ValidDate, &v.Status, &v.ProductName, &v.SoldAs, &v.MainModel, &v.SeriesModels, &v.Issuer, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanAuthorization(row scanner) (Authorization, error) {
	var v Authorization
	err := row.Scan(&v.ID, &v.CertificateID, &v.AuthorizerName, &v.MainModel, &v.AuthorizeeTaxID, &v.AuthorizeeName, &v.AuthorizeeAddr, &v.AuthorizeePhone, &v.ValidDate, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func queryRows[T any](ctx context.Context, s *Store, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []T
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Registration(ctx context.Context, id string) (*Registration, error) {
	v, err := scanRegistration(s.DB.QueryRowContext(ctx, "SELECT "+registrationColumns+" FROM registration WHERE id=?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Certificates, err = queryRows(ctx, s, scanCertificate, "SELECT "+certificateColumns+" FROM certificate WHERE registration_id=? ORDER BY id", id)
	return &v, err
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.DB.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM registration),(SELECT COUNT(*) FROM certificate),(SELECT COUNT(*) FROM authorization)").Scan(&c.Registrations, &c.Certificates, &c.Authorizations)
	return c, err
}

func (s *Store) recentOrder() string {
	if s.Driver == "sqlite" {
		return "CASE WHEN typeof(updated_at)='integer' THEN updated_at ELSE (julianday(updated_at)-2440587.5)*86400000 END"
	}
	return "updated_at"
}

func (s *Store) Recent(ctx context.Context) ([]Registration, error) {
	regs, err := queryRows(ctx, s, scanRegistration, "SELECT "+registrationColumns+" FROM registration ORDER BY "+s.recentOrder()+" DESC,id LIMIT 20")
	if err != nil {
		return nil, err
	}
	for i := range regs {
		if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM certificate WHERE registration_id=?", regs[i].ID).Scan(&regs[i].CertificateCount); err != nil {
			return nil, err
		}
	}
	return regs, nil
}
func (s *Store) All(ctx context.Context) ([]Registration, error) {
	return queryRows(ctx, s, scanRegistration, "SELECT "+registrationColumns+" FROM registration ORDER BY id")
}
func (s *Store) RefreshStats(ctx context.Context) ([]RefreshStat, error) {
	return queryRows(ctx, s, func(row scanner) (RefreshStat, error) {
		var v RefreshStat
		var updated, checked Timestamp
		err := row.Scan(&v.ID, &updated, &checked, &v.Checks, &v.Changes, &v.CheckSeconds)
		v.SyncedAt = updated.Time
		if checked.After(v.SyncedAt) {
			v.SyncedAt = checked.Time
		}
		return v, err
	}, "SELECT id,updated_at,checked_at,check_count,change_count,check_seconds FROM registration ORDER BY id")
}

// RecordCheck adds one BSMI change check covering the given number of seconds.
func (s *Store) RecordCheck(ctx context.Context, id string, changed bool, seconds int64) error {
	change := 0
	if changed {
		change = 1
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE registration SET checked_at=?,check_count=check_count+1,change_count=change_count+?,check_seconds=check_seconds+? WHERE id=?", s.now(), change, seconds, id)
	return err
}

func (s *Store) ByTaxID(ctx context.Context, id string) ([]Registration, error) {
	return queryRows(ctx, s, scanRegistration, "SELECT "+registrationColumns+" FROM registration WHERE tax_id=? ORDER BY id", id)
}
func (s *Store) AuthorizationsByTaxID(ctx context.Context, id string) ([]Authorization, error) {
	return queryRows(ctx, s, scanAuthorization, "SELECT "+authorizationColumns+" FROM authorization WHERE authorizee_tax_id=? ORDER BY id", id)
}
func (s *Store) AuthorizationsByRegistration(ctx context.Context, id string) ([]Authorization, error) {
	return queryRows(ctx, s, scanAuthorization, "SELECT "+authorizationColumns+" FROM authorization WHERE certificate_id IN (SELECT id FROM certificate WHERE registration_id=?) ORDER BY id", id)
}

func searchClause(columns, query string) (string, []any) {
	var terms []string
	var args []any
	for _, col := range strings.Split(columns, ",") {
		terms = append(terms, col+" LIKE ?")
		args = append(args, "%"+query+"%")
	}
	return strings.Join(terms, " OR "), args
}
func (s *Store) Search(ctx context.Context, q string) ([]Registration, []Certificate, []Authorization, error) {
	where, args := searchClause("id,tax_id,applicant,contact_addr,company_addr,phone,note", q)
	regs, err := queryRows(ctx, s, scanRegistration, "SELECT "+registrationColumns+" FROM registration WHERE "+where+" ORDER BY id LIMIT 100", args...)
	if err != nil {
		return nil, nil, nil, err
	}
	where, args = searchClause("id,product_name,main_model,series_models,issuer", q)
	certs, err := queryRows(ctx, s, scanCertificate, "SELECT "+certificateColumns+" FROM certificate WHERE "+where+" ORDER BY id LIMIT 100", args...)
	if err != nil {
		return nil, nil, nil, err
	}
	where, args = searchClause("id,certificate_id,authorizer_name,main_model,authorizee_tax_id,authorizee_name,authorizee_addr,authorizee_phone", q)
	auths, err := queryRows(ctx, s, scanAuthorization, "SELECT "+authorizationColumns+" FROM authorization WHERE "+where+" ORDER BY id LIMIT 100", args...)
	return regs, certs, auths, err
}

func (s *Store) now() any {
	if s.Driver == "sqlite" {
		return time.Now().UnixMilli()
	}
	return time.Now().UTC()
}

func (s *Store) upsertSQL(table, columns, update string) string {
	fields := strings.Split(columns, ",")
	assignments := []string{}
	for _, col := range strings.Split(update, ",") {
		value := "excluded." + col
		if s.Driver == "mysql" {
			value = "VALUES(" + col + ")"
		}
		assignments = append(assignments, col+"="+value)
	}
	conflict := " ON CONFLICT(id) DO UPDATE SET "
	if s.Driver == "mysql" {
		conflict = " ON DUPLICATE KEY UPDATE "
	}
	return "INSERT INTO " + table + " (" + columns + ") VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(fields)), ",") + ")" + conflict + strings.Join(assignments, ",")
}

func (s *Store) SaveRegistration(ctx context.Context, r *Registration) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now()
	query := s.upsertSQL("registration", registrationColumns, "tax_id,applicant,contact_addr,company_addr,phone,note,updated_at")
	if _, err = tx.ExecContext(ctx, query, r.ID, r.TaxID, r.Applicant, r.ContactAddr, r.CompanyAddr, r.Phone, r.Note, now, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM certificate WHERE registration_id=?", r.ID); err != nil {
		return err
	}
	query = s.upsertSQL("certificate", certificateColumns, "registration_id,valid_date,status,product_name,sold_as,main_model,series_models,issuer,updated_at")
	seen := map[string]bool{}
	for _, c := range r.Certificates {
		if c.ID == "" || seen[c.ID] {
			return fmt.Errorf("empty or duplicate certificate ID %q", c.ID)
		}
		seen[c.ID] = true
		if _, err = tx.ExecContext(ctx, query, c.ID, r.ID, c.ValidDate, c.Status, c.ProductName, c.SoldAs, c.MainModel, c.SeriesModels, c.Issuer, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ImportCertificates uses a dedicated connection so foreign key checks are
// disabled and restored on the same session, including every failure path.
func (s *Store) ImportCertificates(ctx context.Context, certs []Certificate) (err error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	off, on := "PRAGMA foreign_keys=OFF", "PRAGMA foreign_keys=ON"
	if s.Driver == "mysql" {
		off, on = "SET FOREIGN_KEY_CHECKS=0", "SET FOREIGN_KEY_CHECKS=1"
	}
	if _, err = conn.ExecContext(ctx, off); err != nil {
		return err
	}
	defer func() {
		resetCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, resetErr := conn.ExecContext(resetCtx, on); resetErr != nil {
			// Discard the session rather than returning disabled checks to the pool.
			conn.Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				err = resetErr
			}
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := s.upsertSQL("certificate", certificateColumns, "valid_date,status,product_name,main_model,updated_at")
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := s.now()
	for _, c := range certs {
		if _, err = stmt.ExecContext(ctx, c.ID, "", c.ValidDate, c.Status, c.ProductName, "", c.MainModel, "", "", now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ReplaceAuthorizations(ctx context.Context, auths []Authorization) error {
	if len(auths) == 0 {
		return fmt.Errorf("refusing to replace authorizations with an empty dataset")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM authorization"); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO authorization ("+authorizationColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := s.now()
	for _, a := range auths {
		if _, err = stmt.ExecContext(ctx, a.ID, a.CertificateID, a.AuthorizerName, a.MainModel, a.AuthorizeeTaxID, a.AuthorizeeName, a.AuthorizeeAddr, a.AuthorizeePhone, a.ValidDate, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
