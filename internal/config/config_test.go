package config

import (
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestDatabaseConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		driver    string
		wantError bool
	}{
		{"default", nil, "sqlite", false},
		{"sqlite URL", map[string]string{"DATABASE_URL": "file:./prisma/dev.db"}, "sqlite", false},
		{"MySQL URL", map[string]string{"DATABASE_URL": "mysql://user:p%40ss%3Aword@localhost/db"}, "mysql", false},
		{"production adapter", map[string]string{"NODE_ENV": "production", "DATABASE_URL": "file:dev.db", "MYSQL_HOST": "localhost", "MYSQL_DATABASE": "db", "MYSQL_USER": "user", "MYSQL_PASSWORD": "p@ss:word"}, "mysql", false},
		{"explicit", map[string]string{"BSMI_DRIVER": "sqlite", "BSMI_DSN": ":memory:", "NODE_ENV": "production"}, "sqlite", false},
		{"missing production DB", map[string]string{"NODE_ENV": "production"}, "", true},
		{"partial explicit", map[string]string{"BSMI_DRIVER": "mysql"}, "", true},
		{"unsupported URL", map[string]string{"DATABASE_URL": "postgres://localhost/db"}, "", true},
		{"Prisma options", map[string]string{"DATABASE_URL": "mysql://user:pass@localhost/db?sslaccept=strict"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := FromEnv(func(k string) string { return tc.env[k] })
			if (err != nil) != tc.wantError {
				t.Fatal(err)
			}
			if err != nil {
				return
			}
			if c.Driver != tc.driver {
				t.Fatal(c.Driver)
			}
			if c.Driver == "mysql" {
				mc, err := mysql.ParseDSN(c.DSN)
				if err != nil || mc.Passwd != "p@ss:word" {
					t.Fatal("password was not preserved")
				}
			}
		})
	}
}
