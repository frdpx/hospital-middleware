# Hospital Middleware

A Go service that sits between hospital staff and each hospital's Hospital
Information System (HIS). Staff log in, belong to exactly one hospital, and can
search only that hospital's patients. Patient data is fetched from the
hospital's HIS on demand and normalized into our own schema.

**Stack:** Go 1.25 · Gin · PostgreSQL 16 · Docker Compose · Nginx

---

## Quick start

```bash
cp .env.example .env          # then edit JWT_SECRET
docker compose --profile dev up -d --build
```

The `dev` profile also starts a mock Hospital A HIS, because the real
`https://hospital-a.api.co.th` is not reachable from a development machine.
Without it, `/patient/search` still works against local data but returns
`502 HIS_UNAVAILABLE` when it needs to reach upstream.

Everything is served through nginx on `http://localhost:8080`.

```bash
curl -sS http://localhost:8080/healthz     # {"status":"ok"}
curl -sS http://localhost:8080/readyz      # {"status":"ready"}
```

### End-to-end in three calls

```bash
# 1. create a staff account at Hospital A
curl -sS -X POST http://localhost:8080/staff/create \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}'

# 2. log in
TOKEN=$(curl -sS -X POST http://localhost:8080/staff/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}' \
  | jq -r .access_token)

# 3. search — this patient is not in our DB yet, so it is fetched from the HIS,
#    stored, and returned. Run it twice: the second call is served locally.
curl -sS -X POST http://localhost:8080/patient/search \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"national_id":"1234567890123"}' | jq
```

Seeded mock-HIS patients: `1234567890123` (Somchai Jaidee),
`9876543210987` (Somying Rakdee), passport `AA1234567` (John Doe).

Seeded hospitals: `hospital-a`, `hospital-b`.

### Try the isolation rule

```bash
# a staff member at hospital-b cannot see hospital-a's patient
curl -sS -X POST http://localhost:8080/staff/create -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd456","hospital":"hospital-b"}'   # 201 — same username, different hospital

TOKEN_B=$(curl -sS -X POST http://localhost:8080/staff/login -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd456","hospital":"hospital-b"}' | jq -r .access_token)

curl -sS -X POST http://localhost:8080/patient/search \
  -H "Authorization: Bearer $TOKEN_B" -H 'Content-Type: application/json' \
  -d '{"last_name":"Jaidee"}'        # {"results":[],"count":0}
```

---

## Running without Docker

Needs a reachable PostgreSQL 16.

```bash
go run ./cmd/mockhis &                       # mock HIS on :9090

export POSTGRES_HOST=localhost POSTGRES_PASSWORD=… \
       JWT_SECRET=a-secret-that-is-at-least-32-characters \
       HIS_BASE_URL_OVERRIDE=http://localhost:9090
go run ./cmd/api                             # migrations run on startup
```

## Tests

```bash
make test          # 188 cases, ~4s, no database or Docker required
make test-cover    # per-function coverage
make check         # fmt + vet + test — run before committing
```

Coverage on the layers that matter (measured with `-coverpkg=./internal/...`):

| Package | |
|---|---|
| `service` | 95.5% |
| `handler` | 91.5% |
| `repository` | 90.7% |
| `auth` | 96.3% |
| `middleware` | 86.7% |
| `config` | 93.9% |

Every endpoint has positive and negative cases: invalid input, wrong password,
duplicate username, unknown hospital, missing/malformed/expired/forged token,
cross-hospital access attempts, patient not found, and HIS failures.

## Make targets

```
make help          list every target
make test          run all tests
make check         fmt + vet + test
make up / down     start / tear down the compose stack
make logs / ps     tail logs / show status
```

---

## API

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/staff/create` | POST | — | register a staff account |
| `/staff/login` | POST | — | authenticate, receive a hospital-scoped JWT |
| `/patient/search` | POST | JWT | search patients within the caller's own hospital |
| `/healthz` | GET | — | liveness |
| `/readyz` | GET | — | readiness (checks the database) |

Full request/response and error documentation: **[docs/api-spec.md](docs/api-spec.md)**

## Documentation

| | |
|---|---|
| [docs/api-spec.md](docs/api-spec.md) | every endpoint, field, error code and example |
| [docs/er-diagram.md](docs/er-diagram.md) | schema, Mermaid ER diagram, and why it is shaped that way |
| [docs/structure.md](docs/structure.md) | project layout, dependency rules, testing strategy |
| [docs/adr/](docs/adr/) | the seven decisions worth arguing about, and their trade-offs |

## How the pieces fit

```
client ──► nginx ──► Go API ──► PostgreSQL
                        │
                        └──► HIS adapter ──► hospital's HIS
```

- **nginx** terminates the client connection, rate-limits the credential
  endpoints (10 req/min) and the rest of the API (60 req/s), and propagates
  `X-Request-ID`.
- **The Go API** owns authentication, the hospital scope, and normalization of
  each HIS's payload into one internal patient model.
- **PostgreSQL** stores the canonical person (`patients`) separately from each
  hospital's registration of them (`hospital_patients`) — see
  [ADR-0002](docs/adr/0002-split-patients-from-hospital-patients.md).

### Design decisions in one line each

- **The hospital scope is a signed JWT claim**, not a request field, so a client
  cannot widen it. ([ADR-0004](docs/adr/0004-hospital-scope-lives-in-the-jwt.md))
- **Every patient query starts from `hospital_patients`**, so another
  hospital's data is unreachable rather than merely un-selected.
- **Adding Hospital B** means one new `HISClient` implementation and one row —
  no schema change, no service-layer change.
- **Search is local-first**; the HIS is consulted only for `national_id` /
  `passport_id` lookups, because that is the only lookup it exposes.
  ([ADR-0007](docs/adr/0007-his-fallback-only-for-identifier-searches.md))
- **`/patient/search` is POST** so national ids never land in access logs.
  ([ADR-0006](docs/adr/0006-post-for-patient-search.md))
- **Migrations are embedded in the binary** and applied on startup.
  ([ADR-0005](docs/adr/0005-embedded-migrations-on-startup.md))

## Security notes

- Passwords are bcrypt-hashed; `models.Staff.PasswordHash` is `json:"-"` so it
  cannot leak through a handler by accident.
- Login returns one identical `401 INVALID_CREDENTIALS` for a wrong password, an
  unknown username, an unknown hospital, and a right-user-wrong-hospital
  attempt — and burns comparable time on each, so the endpoint is not an
  account-enumeration oracle.
- The JWT parser pins the signing algorithm to HS256, blocking `alg: none`
  forgery.
- Error responses never carry SQL text, upstream URLs or connection details;
  those go to the structured logs only.
- Access logs deliberately record no request bodies or query strings — they
  would contain national ids and patient names.
- The service runs as a non-root user in a static, multi-stage-built image.
- **`/staff/create` is unauthenticated** so the assignment is demonstrable end
  to end. This is the one deliberate deviation from what production should do,
  and it is argued explicitly in
  [ADR-0003](docs/adr/0003-staff-create-is-unauthenticated.md).

## Known limitations

- Name search uses `ILIKE '%term%'`, which cannot use a btree index for a
  leading wildcard. Fine at this scale; `pg_trgm` GIN indexes are the fix.
- `synced_at` is recorded but not yet used to re-fetch stale records — today
  the HIS is only consulted when a record is *absent*.
- No refresh tokens; a token simply expires after `JWT_TTL` (default 1 hour).
- No pagination on search results; they are capped at 100 rows.
