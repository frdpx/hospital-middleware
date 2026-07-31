# Hospital Middleware — Development Planning Document

Repository: https://github.com/frdpx/hospital-middleware

This document covers the three deliverables asked for in the assignment:
**Project Structure**, **API Spec** and **ER Diagram**. It is self-contained —
nothing here requires reading the code first.

---

## 1. Overview

A middleware service that sits between hospital staff and each hospital's
Hospital Information System (HIS).

- Staff belong to exactly one hospital and log in against it.
- Staff can search patients **only within their own hospital**.
- Patient master data is fetched from the hospital's HIS on demand and
  normalized into our own schema.

```
client ──► nginx ──► Go API ──► PostgreSQL
                        │
                        └──► HIS adapter ──► that hospital's HIS
```

**Tech stack (as specified):** Go · Gin · PostgreSQL · Docker + docker-compose · Nginx

### The two problems that shaped the design

**A. One person, many hospitals.** The same human can be a patient at several
hospitals, and each hospital's HIS gives them a *different* local hospital
number (`patient_hn`). Storing "a patient" as one flat row per hospital would
make it impossible to recognise the same person across hospitals.

**B. Strict data isolation.** A staff member must never see another hospital's
patients. This is the requirement most likely to be broken by accident, so it
is enforced by the *shape of the schema and the query*, not by a check that
someone could forget to write.

Both are solved by splitting the patient into two tables — see section 4.

---

## 2. Project Structure

### Layout

```
.
├── .github/workflows/ci.yml     CI: fmt, vet, race tests, govulncheck,
│                                migrations on real Postgres, image builds
├── cmd/
│   ├── api/main.go              composition root — the only file that knows
│   │                            about concrete implementations
│   ├── migrate/main.go          apply or roll back migrations deliberately
│   └── mockhis/main.go          stand-in for the Hospital A HIS (dev only)
├── internal/
│   ├── apierr/                  the one error type: HTTP status + client-safe
│   │                            message + private cause
│   ├── auth/                    JWT issuing and verification
│   ├── config/                  env loading + startup validation
│   ├── db/                      connection pool, embedded migration runner
│   ├── handler/                 Gin handlers, request/response DTOs, router
│   ├── hisclient/               HISClient interface + Hospital A adapter
│   │   └── mockhis/             the mock HIS as a plain http.Handler
│   ├── middleware/              auth, request id, access log, panic recovery
│   ├── models/                  domain types
│   ├── repository/              every SQL statement in the service
│   ├── service/                 business rules; depends only on interfaces
│   └── testutil/                test doubles: in-memory repos and fake HIS
├── migrations/                  numbered SQL, embedded into the binary
├── deploy/nginx/                reverse-proxy config
├── docs/                        this document, api-spec, er-diagram, ADRs
├── Dockerfile                   multi-stage; separate api / mockhis targets
├── docker-compose.yml           nginx + api + postgres (+ dev-profile mockhis)
└── Makefile
```

### The dependency rule

```
handler  ──►  service  ──►  repository  ──►  PostgreSQL
   │             │
   │             └──────►  hisclient  ──►  external HIS
   │
   └──►  middleware  ──►  auth
```

Dependencies point **one way only**. `service` never imports `handler`;
`repository` never imports `service`. The one shared leaf is `models`, which
imports nothing of ours.

### Interfaces are declared by the consumer

`service` declares `HospitalRepository`, `StaffRepository` and
`PatientRepository` as small interfaces *at the point of use*. `repository`
returns concrete structs and does not know those interfaces exist. The same
applies to `hisclient.HISClient`.

Two consequences:

1. Every business rule is unit-testable with an in-memory fake — no database,
   no Docker, no test containers. The whole suite runs in a few seconds.
2. Supporting **Hospital B** means adding one `HISClient` implementation and one
   row in `hospitals`. No schema change, no service-layer change.

### What lives where

| Package | Holds | Does **not** hold |
|---|---|---|
| `handler` | binding, validation of *shape*, DTO ↔ model mapping, status rendering | business rules, SQL |
| `service` | business rules, orchestration, validation of *meaning* | SQL, HTTP concepts |
| `repository` | SQL, driver-error classification | business rules |
| `hisclient` | upstream payload shapes, normalization | storage, business rules |
| `models` | domain types and their invariants | the API's JSON shapes |

The API's JSON shapes are DTOs in `handler`, deliberately separate from
`models`. A column rename therefore cannot silently become a breaking API
change, and `models.Staff.PasswordHash` cannot leak because someone returned a
model directly.

### Error handling — one path

Every error reaches the client the same way:

```
apierr.Validation("date_of_birth must be in YYYY-MM-DD format")
        │
        ▼
respondError(c, logger, err)
        │
        ├── status ≥ 500 → logged with the private cause
        └── rendered as {"error":{"code":…,"message":…}}
```

Anything unclassified becomes a generic `500`, so SQL text and upstream URLs
can never reach a client. The cause travels for the logs only.

### Configuration and migrations

All configuration is environment variables (12-factor), **validated at
startup**: a missing `POSTGRES_PASSWORD`, a too-short `JWT_SECRET`, or a
malformed value like `JWT_TTL=1hour` fails the process immediately with every
problem listed at once.

Migrations are plain SQL files embedded into the binary with `go:embed` and
applied by `golang-migrate` on startup. The image is therefore self-contained —
code and the schema it expects always ship together — and golang-migrate's
advisory lock means several replicas starting at once still apply each
migration exactly once.

### Testing strategy

| Layer | Technique | What it proves |
|---|---|---|
| `hisclient` | `httptest` + the real mock HIS handler | upstream contract, timeouts, malformed responses |
| `service` | in-memory fakes | business rules, including cross-hospital isolation |
| `handler` | full Gin router over `httptest` | middleware, binding, status codes, error envelope |
| `repository` | `sqlmock` | the SQL itself: hospital filter present, values bound |
| `config`, `models`, `auth` | plain unit tests | validation, date handling, token rules |

**210 test cases**, race-clean. Coverage on the layers that matter: service
95%, handler 91%, repository 91%, auth 96%.

---

## 3. API Spec

### Conventions

| | |
|---|---|
| Base URL | `http://localhost:8080` (through nginx) |
| Content type | `application/json` on every request and response |
| Auth | `Authorization: Bearer <jwt>` — required on `/patient/search` only |
| Date format | `YYYY-MM-DD` everywhere |
| Correlation id | `X-Request-ID` echoed on every response |

**Error envelope** — every non-2xx response uses exactly this shape, including
404s from unknown routes and nginx's own 429/503. Clients branch on `code`,
never on `message`.

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "username, password or hospital is incorrect"
  }
}
```

**Error codes**

| Code | Status | Where |
|---|---|---|
| `VALIDATION_ERROR` | 400 | all endpoints |
| `UNAUTHORIZED` | 401 | `/patient/search` |
| `INVALID_CREDENTIALS` | 401 | `/staff/login` |
| `HOSPITAL_NOT_FOUND` | 404 | `/staff/create` |
| `PATIENT_NOT_FOUND` | 404 | `/patient/search` |
| `ROUTE_NOT_FOUND` | 404 | unknown path |
| `METHOD_NOT_ALLOWED` | 405 | known path, wrong verb |
| `USERNAME_TAKEN` | 409 | `/staff/create` |
| `PATIENT_IDENTIFIER_CONFLICT` | 409 | `/patient/search` |
| `RATE_LIMITED` | 429 | all endpoints (nginx) |
| `INTERNAL_ERROR` | 500 | all endpoints |
| `HIS_UNAVAILABLE` | 502 | `/patient/search` |
| `SERVICE_UNAVAILABLE` | 503 | all endpoints (nginx) |

---

### 3.1 `POST /staff/create`

Registers a staff account at an existing hospital. **Auth:** none.

**Request**

```json
{
  "username": "jsmith",
  "password": "P@ssw0rd123",
  "hospital": "hospital-a"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `username` | string | yes | max 64 chars. Unique **within the hospital only** |
| `password` | string | yes | 8–72 characters, stored as a bcrypt hash |
| `hospital` | string | yes | the hospital's `code` or display `name`, case-insensitive |

**Response `201 Created`**

```json
{
  "id": "3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a",
  "username": "jsmith",
  "hospital": "hospital-a",
  "created_at": "2026-07-31T09:00:00Z"
}
```

The password and its hash never appear in any response.

**Errors**

| Status | Code | Scenario |
|---|---|---|
| 400 | `VALIDATION_ERROR` | missing field, password outside 8–72, malformed JSON |
| 404 | `HOSPITAL_NOT_FOUND` | `hospital` matches no row in `hospitals` |
| 409 | `USERNAME_TAKEN` | that username already exists **at that hospital** |

**Note on `password` length:** bcrypt silently truncates at 72 bytes. Rejecting
longer input is safer than accepting a password whose tail is ignored.

---

### 3.2 `POST /staff/login`

Authenticates a staff member and returns a JWT scoped to their hospital.
**Auth:** none.

`hospital` is required, not decorative: usernames are unique only within a
hospital, so the account lookup is always on the `(hospital, username)` pair.
A lookup by username alone could match a different hospital's employee.

**Request**

```json
{
  "username": "jsmith",
  "password": "P@ssw0rd123",
  "hospital": "hospital-a"
}
```

**Response `200 OK`**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

JWT claims (HS256, default lifetime 1 hour):

```json
{
  "sub": "3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a",
  "hospital_id": "8a2b1c3d-4e5f-4a7b-8c9d-0e1f2a3b4c5d",
  "username": "jsmith",
  "iss": "hospital-middleware",
  "iat": 1753868400,
  "exp": 1753872000
}
```

**Errors**

| Status | Code | Scenario |
|---|---|---|
| 400 | `VALIDATION_ERROR` | a required field is missing |
| 401 | `INVALID_CREDENTIALS` | wrong password, unknown username, unknown hospital, or a username that exists at a *different* hospital |

All four 401 cases return an identical body and take comparable time — an
unknown user is still checked against a dummy bcrypt hash. A distinguishable
response would let an attacker enumerate which hospitals and usernames exist.

---

### 3.3 `POST /patient/search`

Searches patients, scoped strictly to the hospital in the caller's token.
**Auth:** required.

`POST` rather than `GET`: the body carries national ids, passport ids and
patient names, and query strings end up in nginx access logs, browser history
and proxy caches.

**Request** — all fields optional, but at least one must be present. Fields
present but blank are treated as absent.

```json
{
  "national_id": "1234567890123",
  "passport_id": null,
  "first_name": "Somchai",
  "middle_name": null,
  "last_name": "Jaidee",
  "date_of_birth": "1990-05-20",
  "phone_number": null,
  "email": null
}
```

| Field | Matching |
|---|---|
| `national_id` | exact |
| `passport_id` | exact |
| `first_name` | case-insensitive substring, against **either** `first_name_th` or `first_name_en`; minimum 2 characters |
| `middle_name` | as above |
| `last_name` | as above |
| `date_of_birth` | exact, `YYYY-MM-DD` |
| `phone_number` | exact |
| `email` | case-insensitive exact |

**There is no `hospital` field.** The scope comes from the signed token;
sending one in the body has no effect.

**Behaviour**

1. Query local storage, always filtered by the token's `hospital_id`.
2. If there are matches, return them.
3. If there are none **and** the search included `national_id` or
   `passport_id` **and** that identifier is not already on file for this
   hospital, call the hospital's HIS, store the result (`patients` +
   `hospital_patients`, in one transaction), re-run the same scoped query and
   return the result.
4. If there are none and the search was by name/phone/email only, return an
   empty list — the HIS exposes no way to search by those fields.

Consequences worth knowing:

- A name-only search that matches nothing is `200 {"results":[],"count":0}`,
  **not** a 404.
- An identifier search that matches nothing locally **or** at the HIS is
  `404 PATIENT_NOT_FOUND`.
- A patient belonging only to another hospital is never returned, on any path.
- Results are capped at 100 rows.

**Response `200 OK`**

```json
{
  "results": [
    {
      "patient_hn": "HN00123",
      "national_id": "1234567890123",
      "passport_id": null,
      "first_name_th": "สมชาย",
      "middle_name_th": null,
      "last_name_th": "ใจดี",
      "first_name_en": "Somchai",
      "middle_name_en": null,
      "last_name_en": "Jaidee",
      "date_of_birth": "1990-05-20",
      "phone_number": "0812345678",
      "email": "somchai.jaidee@example.com",
      "gender": "M"
    }
  ],
  "count": 1
}
```

`patient_hn` is the HN **this hospital** uses. The same person searched from
another hospital returns a different `patient_hn`.

**Errors**

| Status | Code | Scenario |
|---|---|---|
| 400 | `VALIDATION_ERROR` | no search fields, bad `date_of_birth` format, name filter under 2 characters |
| 401 | `UNAUTHORIZED` | token missing, malformed, expired, wrongly signed, or carrying no hospital scope |
| 404 | `PATIENT_NOT_FOUND` | identifier search found nothing locally or at the HIS |
| 409 | `PATIENT_IDENTIFIER_CONFLICT` | the HIS returned an identifier that already belongs to a different patient |
| 502 | `HIS_UNAVAILABLE` | HIS timed out, refused the connection, returned 5xx, or returned an unusable body |

---

### 3.4 Upstream: the Hospital A HIS

```
GET https://hospital-a.api.co.th/patient/search/{id}
```

`{id}` is a `national_id` or a `passport_id`. Response fields consumed:
`first_name_th`, `middle_name_th`, `last_name_th`, `first_name_en`,
`middle_name_en`, `last_name_en`, `date_of_birth`, `patient_hn`, `national_id`,
`passport_id`, `phone_number`, `email`, `gender` (M/F).

The adapter normalizes this into our internal model: empty strings become
`NULL`, gender and dates are normalized, the identifier is path-escaped, and
the response body is size-capped. `404` from the HIS means "no such patient"
(→ our 404); anything else means "unavailable" (→ our 502).

That endpoint is not reachable from a development machine, so the repository
ships a mock implementing the same contract, used by both the unit tests and
the `dev` docker-compose profile.

### 3.5 Operational endpoints

| Endpoint | Purpose | Success |
|---|---|---|
| `GET /healthz` | liveness | `200 {"status":"ok"}` |
| `GET /readyz` | readiness (checks the database) | `200 {"status":"ready"}` / `503` |

---

## 4. ER Diagram

![ER diagram](assets/er-diagram.png)

*(Image file: `docs/assets/er-diagram.png`. Source: `docs/assets/er-diagram.mmd`.)*

### Tables

**`hospitals`** — reference data, provisioned by an operator, never created
through the API.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid | PK |
| `code` | text | unique; the `hospital` field of the staff APIs, e.g. `hospital-a` |
| `name` | text | unique; display name, e.g. `Hospital A` |
| `his_adapter_type` | text | which `HISClient` implementation to use |
| `his_base_url` | text | nullable; that hospital's HIS endpoint |
| `created_at`, `updated_at` | timestamptz | |

**`staff`** — a hospital employee account.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid | PK |
| `hospital_id` | uuid | FK → `hospitals` |
| `username` | text | unique **within `hospital_id`**, not globally |
| `password_hash` | text | bcrypt |
| `created_at`, `updated_at` | timestamptz | |

**`patients`** — the canonical record of a person, independent of any hospital.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid | PK |
| `national_id` | text | nullable, unique when present |
| `passport_id` | text | nullable, unique when present |
| `first_name_th`, `last_name_th` | text | |
| `middle_name_th` | text | nullable |
| `first_name_en`, `last_name_en` | text | |
| `middle_name_en` | text | nullable |
| `date_of_birth` | date | nullable |
| `phone_number`, `email` | text | nullable |
| `gender` | text | `M`, `F` or empty |
| `created_at`, `updated_at` | timestamptz | |

**`hospital_patients`** — links a person to one hospital, with that hospital's
local HN.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid | PK |
| `hospital_id` | uuid | FK → `hospitals` |
| `patient_id` | uuid | FK → `patients` |
| `patient_hn` | text | hospital-local HN from that hospital's HIS |
| `synced_at` | timestamptz | last refresh from the HIS |
| `created_at` | timestamptz | |

### Why `patients` is split from `hospital_patients`

The same person can be a patient at several hospitals, and each hospital's HIS
assigns them its own `patient_hn`. A single flat table keyed by
`(hospital_id, hn)` would mean no way to recognise "this is the same human"
across hospitals, and demographics duplicated and drifting per hospital.

Splitting gives each table one job:

| Table | Answers |
|---|---|
| `patients` | *Who is this person?* Keyed by identifiers unique across all hospitals. |
| `hospital_patients` | *Does this hospital know them, and what do they call them?* |

It also turns the assignment's core access rule into a property of the schema.
`hospital_patients.hospital_id` is the scoping column, and **every patient
query starts from `hospital_patients`**:

```sql
FROM hospital_patients hp
JOIN patients p ON p.id = hp.patient_id
WHERE hp.hospital_id = $1   -- from the JWT, always present
  AND ...
```

A patient row that exists only for another hospital is **not reachable by that
statement at all** — there is no code path, including the HIS fallback, that
can return it.

### Why `staff.username` is unique per hospital

The assignment's `/staff/login` takes `hospital` as an input. That only makes
sense if `username` alone cannot identify an account — so two hospitals may
each employ a `jsmith`. Enforced by a composite unique index on
`(hospital_id, lower(username))`, and the login lookup is always on the pair.

### Why `hospitals` carries `his_adapter_type` and `his_base_url`

Only Hospital A is specified today, but the wording implies Hospital B, C…
each with their own payload shape. Storing *which adapter* a hospital uses
means supporting a new HIS is: add a `HISClient` implementation, add a case in
the factory, insert a row.

### Indexes and constraints

| Index / constraint | Purpose |
|---|---|
| `ux_hospitals_code`, `ux_hospitals_name` | `hospital` may be given as either form |
| `ux_staff_hospital_username` on `(hospital_id, lower(username))` | usernames unique per hospital |
| `ix_staff_hospital_id` | every login and search filters by hospital |
| `ux_patients_national_id` (partial) | one canonical person per national id |
| `ux_patients_passport_id` (partial) | one canonical person per passport id |
| `ck_patients_has_identifier` | a person must have a national id **or** a passport id |
| `ck_patients_gender` | `M`, `F` or empty |
| `ux_hospital_patients_hospital_patient` | a patient is registered once per hospital |
| `ux_hospital_patients_hospital_hn` | an HN is unique within a hospital |
| `ix_patients_*` on `lower(name)`, dob, phone, `lower(email)` | back the search filters |

Uniqueness on the identifiers uses **partial** indexes
(`WHERE national_id IS NOT NULL`), so many patients can have a NULL passport id
while every present one stays unique.

---

## 5. Infrastructure

`docker-compose.yml` defines the three required services, each waiting for its
dependency to report **healthy** rather than merely started:

| Service | Image | Role |
|---|---|---|
| `nginx` | `nginx:1.27-alpine` | reverse proxy, rate limiting, security headers |
| `api` | built from `Dockerfile` (target `api`) | the Go service |
| `postgres` | `postgres:16-alpine` | database, bound to `127.0.0.1` only |

An optional `dev` profile adds `mockhis`, the Hospital A stand-in, built from
its own image target so no development stand-in exists inside the production
image.

**nginx** rate-limits the credential endpoints to 30 req/min and the rest of
the API to 60 req/s, caps request bodies at 64 KB, sets security headers,
propagates `X-Request-ID`, and renders its own 429/503 in the same JSON
envelope the API uses.

**The Go image** is a multi-stage build producing a static CGO-free binary on
Alpine, running as a non-root user, with a built-in healthcheck.

### Running it

```bash
cp .env.example .env          # then edit JWT_SECRET
docker compose --profile dev up -d --build
```

Everything is served through nginx on `http://localhost:8080`.

```bash
# 1. create a staff account
curl -X POST http://localhost:8080/staff/create \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}'

# 2. log in
TOKEN=$(curl -s -X POST http://localhost:8080/staff/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}' \
  | jq -r .access_token)

# 3. search — not in our DB yet, so it is fetched from the HIS and stored
curl -X POST http://localhost:8080/patient/search \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"national_id":"1234567890123"}'
```

### CI

GitHub Actions runs on every push: gofmt, `go vet`, the full test suite under
the race detector with coverage, `govulncheck`, migrations applied **and rolled
back** against a real Postgres, both image builds, `docker compose config` and
`nginx -t`.

---

## 6. Security notes

- Passwords are bcrypt-hashed; the hash is tagged `json:"-"` so it cannot leak
  through a handler by accident.
- Login returns one identical `401` for every failure mode and burns comparable
  time on each, so the endpoint is not an account-enumeration oracle.
- The JWT parser pins the signing algorithm to HS256, blocking `alg: none`
  forgery. The hospital scope is a signed claim, so a client cannot widen it.
- Error responses never carry SQL text, upstream URLs or connection details.
- Access logs deliberately record no request bodies or query strings — they
  would contain national ids and patient names.
- The service runs as a non-root user in a static, multi-stage-built image.
- **`/staff/create` is unauthenticated**, so the assignment is demonstrable end
  to end from a clean database. This is the one deliberate deviation from what
  production should do; in production it would require an admin token or move
  to an internal admin surface.
- **There is no TLS in this stack.** nginx terminates plain HTTP and is meant
  to sit behind a load balancer or ingress that terminates TLS.

## 7. Known limitations

- Name search uses `ILIKE '%term%'`, which cannot use a btree index for a
  leading wildcard. Fine at this scale; `pg_trgm` GIN indexes are the fix.
- `synced_at` is recorded but not yet used to re-fetch stale records — the HIS
  is only consulted when an identifier is *absent* for that hospital.
- A HIS sync merges into the stored record and can never clear a field, so a
  value genuinely deleted upstream persists until an operator removes it.
  Preserving a stale value beats letting a thinner HIS erase a good one.
- No refresh tokens; a token simply expires after its TTL.
- No pagination on search results; they are capped at 100 rows.

---

## Appendix — decision records

Longer rationale for the decisions above lives in `docs/adr/`:

| # | Decision |
|---|---|
| 0001 | A layered monolith with consumer-side interfaces |
| 0002 | Split `patients` from `hospital_patients` |
| 0003 | `/staff/create` is left unauthenticated (with a stated caveat) |
| 0004 | The hospital scope lives in the JWT, not in the request |
| 0005 | Migrations are embedded and applied on API startup |
| 0006 | `/patient/search` is POST, not GET |
| 0007 | Local-first search; the HIS is only consulted for identifier lookups |
