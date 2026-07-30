# Project Structure

## Layout

```
.
├── cmd/
│   ├── api/main.go              composition root — the only file that knows
│   │                            about concrete implementations
│   └── mockhis/main.go          standalone mock of the Hospital A HIS
├── internal/
│   ├── apierr/                  the one error type that carries HTTP status +
│   │                            client-safe message + private cause
│   ├── auth/                    JWT issuing and verification
│   ├── config/                  env loading + startup validation
│   ├── db/                      connection pool, embedded migration runner
│   ├── handler/                 Gin handlers, request/response DTOs, router
│   ├── hisclient/               HISClient interface, Hospital A adapter,
│   │   └── mockhis/             fakes, and the mock HIS http.Handler
│   ├── middleware/              auth, request id, access log, recovery
│   ├── models/                  domain types (no JSON-of-convenience, no SQL)
│   ├── repository/              every SQL statement in the service
│   ├── service/                 business rules; depends only on interfaces
│   └── testutil/                behavioural in-memory repo fakes for tests
├── migrations/                  numbered SQL files, embedded into the binary
├── deploy/nginx/                reverse-proxy config
├── docs/
│   ├── api-spec.md
│   ├── er-diagram.md
│   ├── structure.md             (this file)
│   └── adr/                     architecture decision records
├── Dockerfile                   multi-stage build
├── docker-compose.yml           nginx + api + postgres (+ dev-profile mockhis)
└── Makefile
```

## The dependency rule

```
handler  ──►  service  ──►  repository  ──►  database
   │             │
   │             └──────►  hisclient  ──►  external HIS
   │
   └──►  middleware ──► auth
```

Dependencies point **one way only**. `service` never imports `handler`;
`repository` never imports `service`. The one shared leaf is `models`, which
imports nothing of ours.

### Interfaces are declared by the consumer

`service` declares `HospitalRepository`, `StaffRepository` and
`PatientRepository` as small interfaces at the point of use. `repository`
returns concrete structs and does not know those interfaces exist.

Two things follow:

1. Every business rule is unit-testable with an in-memory fake — no database,
   no Docker, no test containers. The whole suite runs in about 4 seconds.
2. The interface only lists the methods that layer actually needs, so a change
   in the repository does not ripple through mocks that never used it.

`hisclient.HISClient` and `hisclient.Factory` follow the same shape, which is
what lets a second hospital's HIS be added without touching the service layer.

## What lives where, and why

| Package | Holds | Does **not** hold |
|---|---|---|
| `handler` | binding, validation of *shape*, DTO ↔ model mapping, status rendering | business rules, SQL |
| `service` | business rules, orchestration, validation of *meaning* | SQL, HTTP concepts |
| `repository` | SQL, driver-error classification | business rules |
| `hisclient` | upstream payload shapes, normalization | storage, business rules |
| `models` | domain types and their invariants | JSON DTOs for the API surface |

The API's JSON shapes are DTOs in `handler`, deliberately separate from
`models`. That way a column rename does not silently become a breaking API
change, and `models.Staff.PasswordHash` cannot leak just because someone
returned the model directly.

## Error handling: one path

There is exactly one way an error reaches a client:

```go
apierr.Validation("date_of_birth must be in YYYY-MM-DD format")   // in service/handler
        │
        ▼
respondError(c, logger, err)                                       // in handler
        │
        ├── status ≥ 500 → logged with the private cause
        └── rendered as {"error":{"code":…,"message":…}}
```

`apierr.From(err)` maps anything unclassified to a generic 500, so an
unexpected internal error can never render SQL text or an upstream URL to a
client. The cause travels for the logs via `errors.Unwrap`, never in the body.

## Configuration

All configuration is environment variables (12-factor), loaded and **validated
at startup** by `internal/config`. A missing `POSTGRES_PASSWORD` or a
too-short `JWT_SECRET` fails the process immediately with every problem listed
at once, rather than at the first request that needs it.

See [`.env.example`](../.env.example) for the full set.

## Migrations

SQL files in `migrations/` are embedded into the binary with `go:embed` and
applied by `golang-migrate` on API startup (`DB_AUTO_MIGRATE=true`).

- The deployed image is self-contained: no migration CLI, no mounted volume,
  no risk of running SQL from a different build than the code.
- golang-migrate takes a Postgres advisory lock, so several API replicas
  starting together still apply each migration exactly once.
- Set `DB_AUTO_MIGRATE=false` and run migrations as a separate step if your
  environment requires a human-gated schema change.

Reasoning and the alternatives considered:
[ADR-0005](adr/0005-embedded-migrations-on-startup.md).

## Testing strategy

| Layer | Technique | What it proves |
|---|---|---|
| `hisclient` | `httptest` + the real mock HIS handler | the upstream contract, timeouts, malformed responses |
| `service` | in-memory fakes from `testutil` | business rules, including cross-hospital isolation |
| `handler` | full Gin router over `httptest` | middleware, binding, status codes, error envelope |
| `repository` | `sqlmock` | the SQL itself: hospital filter present, values bound, `23505` → 409 |
| `config`, `models` | plain unit tests | validation and date handling |

`testutil`'s fakes are *behavioural*, not stubs — `PatientRepo.Search` really
filters by hospital. That matters: if someone deleted the hospital filter from
the production query, the cross-hospital tests would still fail, because the
fake would happily return the other hospital's row.

Run them:

```bash
make test           # all tests
make test-cover     # with a coverage report
```
