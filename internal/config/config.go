package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Config struct {
	Driver, DSN, HTTPAddr                 string
	SentryWeb, SentryCron, SentryFrontend string
	Location                              *time.Location
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	return FromEnv(os.Getenv)
}

func FromEnv(get func(string) string) (Config, error) {
	c := Config{Driver: get("BSMI_DRIVER"), DSN: get("BSMI_DSN"), HTTPAddr: get("BSMI_HTTP_ADDR"), SentryWeb: get("SENTRY_DSN_WEB"), SentryCron: get("SENTRY_DSN_CRON"), SentryFrontend: get("SENTRY_DSN_FRONTEND"), Location: time.Local}
	if c.HTTPAddr == "" {
		port := get("PORT")
		if port == "" {
			port = "3000"
		}
		c.HTTPAddr = ":" + port
	}
	if tz := get("TZ"); tz != "" {
		var err error
		c.Location, err = time.LoadLocation(tz)
		if err != nil {
			return c, fmt.Errorf("TZ: %w", err)
		}
	}
	if c.Driver != "" || c.DSN != "" {
		if c.DSN == "" || (c.Driver != "mysql" && c.Driver != "sqlite") {
			return c, fmt.Errorf("set both BSMI_DRIVER (mysql or sqlite) and BSMI_DSN")
		}
		return c, nil
	}
	raw := get("DATABASE_URL")
	// Preserve the production Prisma adapter's MYSQL_* settings, even when a
	// development DATABASE_URL is also present in the environment.
	if get("MYSQL_HOST") != "" && (get("NODE_ENV") == "production" || raw == "") {
		if get("MYSQL_DATABASE") == "" {
			return c, fmt.Errorf("MYSQL_DATABASE is required")
		}
		mc := mysql.NewConfig()
		mc.User, mc.Passwd, mc.DBName = get("MYSQL_USER"), get("MYSQL_PASSWORD"), get("MYSQL_DATABASE")
		port := get("MYSQL_PORT")
		if port == "" {
			port = "3306"
		}
		mc.Net, mc.Addr = "tcp", net.JoinHostPort(get("MYSQL_HOST"), port)
		c.Driver, c.DSN = "mysql", mc.FormatDSN()
		return c, nil
	}
	if strings.HasPrefix(raw, "mysql://") {
		u, err := url.Parse(raw)
		if err != nil {
			return c, fmt.Errorf("invalid DATABASE_URL")
		}
		if u.Hostname() == "" || strings.TrimPrefix(u.Path, "/") == "" {
			return c, fmt.Errorf("DATABASE_URL requires a host and database")
		}
		mc := mysql.NewConfig()
		if u.User != nil {
			mc.User = u.User.Username()
			mc.Passwd, _ = u.User.Password()
		}
		mc.DBName = strings.TrimPrefix(u.Path, "/")
		port := u.Port()
		if port == "" {
			port = "3306"
		}
		mc.Net, mc.Addr = "tcp", net.JoinHostPort(u.Hostname(), port)
		// Prisma URL options have different semantics from Go driver options.
		if u.RawQuery != "" {
			return c, fmt.Errorf("DATABASE_URL query options are unsupported; use BSMI_DRIVER=mysql and a Go BSMI_DSN")
		}
		c.Driver, c.DSN = "mysql", mc.FormatDSN()
		return c, nil
	}
	if raw != "" && !strings.HasPrefix(raw, "file:") {
		return c, fmt.Errorf("DATABASE_URL must start with file: or mysql://")
	}
	if raw == "" {
		if get("NODE_ENV") == "production" {
			return c, fmt.Errorf("production database configuration is missing")
		}
		raw = "file:./prisma/dev.db"
	}
	c.Driver, c.DSN = "sqlite", raw
	return c, nil
}
