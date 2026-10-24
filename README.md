# bsmiweb [![CI](https://github.com/hasname/bsmiweb/actions/workflows/ci.yml/badge.svg)](https://github.com/hasname/bsmiweb/actions/workflows/ci.yml)

A BSMI registration search site built with Go and Gin. Like `fama`, it uses
`database/sql`, MySQL in production, SQLite for development, embedded templates,
and [golang-migrate](https://github.com/golang-migrate/migrate) for migrations.

## Development

Use Go 1.27.3 (the same version as `fama`) and GNU Make:

```sh
cp .env.example .env
make install
make run-engineering
```

The server listens on port 3000. `make run-engineering` runs migrations first.
`make test` runs offline tests with disposable SQLite databases; `make lint`
checks formatting and runs `go vet`. `make ci` also runs race detection and
builds the static Linux/amd64 deployment binary at `dist/bsmiweb`.

The binary contains templates, static assets, SQL migrations, and timezone data.
Node.js is only used to regenerate the checked-in Sentry browser bundle with
`make assets`; it is not needed to build or run the Go application. The browser
dependencies remain pinned in `package-lock.json`.

## Configuration

The application loads `.env` from its working directory without overriding
existing environment variables. Database selection, in order:

1. `BSMI_DRIVER` (`mysql` or `sqlite`) together with `BSMI_DSN`.
2. Existing `MYSQL_HOST`, `MYSQL_PORT`, `MYSQL_DATABASE`, `MYSQL_USER`, and
   `MYSQL_PASSWORD` when `NODE_ENV=production`, or when `DATABASE_URL` is absent.
3. `DATABASE_URL=mysql://user:password@host:3306/database` or a SQLite `file:` URL.
4. `file:./prisma/dev.db` in development. Production requires database settings.

The production `MYSQL_*` precedence preserves the old Prisma adapter behavior
when the environment also contains a development SQLite URL. For Go driver
options, use `BSMI_DSN`, for example:

```dotenv
BSMI_DRIVER=mysql
BSMI_DSN="user:password@tcp(127.0.0.1:3306)/bsmiweb?tls=true"
```

Prisma URL query options are rejected because they have different semantics
from Go driver options. MySQL connections use `utf8mb4`, UTC timestamps,
`parseTime`, and `multiStatements`. SQLite accepts existing Prisma millisecond
timestamps as well as SQL datetime strings without rewriting them.

`BSMI_HTTP_ADDR` overrides `PORT` (default `3000`). Only loopback reverse proxies
are trusted for `X-Forwarded-Proto`. `SENTRY_DSN_WEB`, `SENTRY_DSN_CRON`, and
`SENTRY_DSN_FRONTEND` retain their existing roles; the browser SDK is self-hosted.

## Commands and collection

```sh
./dist/bsmiweb migrate
./dist/bsmiweb serve
./dist/bsmiweb cron
./dist/bsmiweb collect recent
./dist/bsmiweb collect pchome
./dist/bsmiweb import-certificates
./dist/bsmiweb import-authorizations
./dist/bsmiweb healthcheck
```

`collect` accepts `import` (certificates then authorizations), `recent`, `pchome`,
`momo`, `books`, or `friday`. The cron daemon preserves the daily schedule:

| Time | Job |
| --- | --- |
| 03:00 | Certificate and authorization XML imports |
| 04:00 | Refresh registrations changed within seven days |
| 05:00 | PChome |
| 06:00 | Momo |
| 07:00 | Books |
| 08:00 | friDay |

Times use `TZ`, or the host's local timezone when unset. Set `TZ=Asia/Taipei`
for Taiwan time. A job is skipped if the previous job is still running.
Termination cancels outstanding requests and database work.

The three certificate datasets retain their precedence: electronic products,
other products, then current-year newly issued certificates. Imports preserve
existing registration links and scraped details. Certificates without a known
registration retain the legacy empty `registration_id`; foreign key checks are
temporarily disabled only on the import's dedicated connection and restored
afterward. Refreshing a registration can attach these imported certificates.
Authorization replacements and certificate imports are transactional; malformed
or empty authorization feeds leave the previous data intact.

Outbound crawler requests use `tls-client` v1.17.1 with the Chrome 152 TLS and
HTTP/2 profile, randomized TLS extension order, and a matching User-Agent.
HTTP/3 is disabled. The two-minute request timeout covers redirects and reading
the response body; cancellation still follows the caller's context.
The existing TLS exception for the self-signed `data.bsmi.gov.tw` endpoint is
checked on every redirect hop. Other outbound requests still verify certificates.

## Migrating from Prisma

`bsmiweb migrate` is the only application command that changes migration state.
`serve`, `cron`, and imports require a clean, current `schema_migrations` version.
Stop both existing services and any manually started imports before the first
transition. Back up the database and `.env` before running it.

The original MySQL SQL files remain unchanged in `prisma/migrations/` as a
historical reference. Identical copies are embedded under
`internal/db/migrations/mysql/`. Their versions map directly:

| Prisma migration | golang-migrate version |
| --- | --- |
| `0001_baseline` | 1 |
| `0002_rename_to_snake_case` | 2 |
| `0003_remove_empty_registrations` | 3 |

On an existing MySQL database, the command checks completed Prisma migrations,
their SHA-256 checksums and contiguous order, then verifies the tables, columns,
indexes, and foreign key before recording that version in `schema_migrations`.
Only pending migrations execute. `_prisma_migrations` is retained unchanged.
For example, a database at version 2 runs the empty-registration cleanup;
a database already at version 3 keeps its data unchanged.

An existing SQLite database created by `prisma db push` has no Prisma migration
history. Its snake_case schema must match before it is adopted at version 2
and the cleanup runs. A fresh database runs all embedded migrations normally.
Repeated runs are safe. The cleanup preserves registrations with any vendor
information or a referenced certificate.

Incomplete Prisma migrations, unknown migration names, checksum mismatches,
unexpected schemas, and dirty golang-migrate versions stop the command. They
are never automatically forced clean. Resolve the reported discrepancy against
the database backup and the original migration SQL before retrying. An existing
MySQL schema without a valid Prisma history must be reconciled using the old
release first; this command will not infer its version from table names alone.

For subsequent changes, add a new numbered `.up.sql` under both dialect
directories and update `db.Version` in `internal/db/db.go`. Do not edit or
rebaseline an applied migration.

## Deployment and rollback

`make deploy` builds the binary, decrypts the existing credentials repository,
and runs `ansible/deploy.yml`. It uploads a staged binary, stops the existing
web and cron services, runs migration, then installs and starts the Go services.
The health check verifies HTTP and database availability. No Node.js, Prisma,
or Go installation is required on the server. The systemd user manager and its
existing linger configuration must be available, as with the previous deploy.

Migration failure stops deployment and leaves services stopped for inspection.
The previous running binary is not replaced until migration succeeds. Later Go
deployments preserve it as `bsmiweb.previous`. Remote legacy source files are
left in place during the initial transition.

For rollback to the Node release, restore that release's sources, dependencies,
environment, and systemd units. If the Go migration applied pending SQL, restore
the database backup as well: its Prisma history deliberately has not been
advanced by Go. In particular, the empty-registration cleanup cannot be undone
without a backup. Do not run Prisma migrations against a database now managed
by golang-migrate. There are no automatic down migrations.

## MySQL/MariaDB integration tests

CI tests migrations against both MySQL 8.4 and MariaDB 11.8. To run the same
tests locally, point the following variable at a disposable server whose user
can create and drop databases:

```sh
BSMI_TEST_MYSQL_DSN='root:test@tcp(127.0.0.1:3306)/' go test -race ./internal/db
```

The tests create uniquely named `bsmiweb_test_*` databases and remove only those
databases afterward. They cover fresh installs, every Prisma baseline version,
failed or inconsistent histories, schema drift, repeated runs, and data
preservation. Without this variable, MySQL integration tests are skipped.

## License

See [LICENSE](LICENSE) for details.
