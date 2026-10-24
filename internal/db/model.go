package db

import (
	"fmt"
	"strconv"
	"time"
)

type Registration struct {
	ID, TaxID, Applicant, ContactAddr, CompanyAddr, Phone, Note string
	CreatedAt, UpdatedAt                                        Timestamp
	Certificates                                                []Certificate
	CertificateCount                                            int
}

type Certificate struct {
	ID, RegistrationID, ValidDate, Status, ProductName, SoldAs, MainModel, SeriesModels, Issuer string
	CreatedAt, UpdatedAt                                                                        Timestamp
}

type Authorization struct {
	ID, CertificateID, AuthorizerName, MainModel, AuthorizeeTaxID, AuthorizeeName, AuthorizeeAddr, AuthorizeePhone, ValidDate string
	CreatedAt, UpdatedAt                                                                                                      Timestamp
}

type Counts struct{ Registrations, Certificates, Authorizations int }

// Prisma SQLite stores epoch milliseconds; older raw imports use SQL datetime
// strings. Accept both representations without rewriting existing data.
type Timestamp struct{ time.Time }

func (t *Timestamp) Scan(value any) error {
	switch v := value.(type) {
	case time.Time:
		t.Time = v.UTC()
		return nil
	case int64:
		t.Time = time.UnixMilli(v).UTC()
		return nil
	case []byte:
		return t.Scan(string(v))
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return t.Scan(n)
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
			if parsed, err := time.Parse(layout, v); err == nil {
				t.Time = parsed.UTC()
				return nil
			}
		}
	}
	return fmt.Errorf("unsupported database timestamp %T: %v", value, value)
}
