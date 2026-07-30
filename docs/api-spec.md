# API Spec — Hospital Middleware

## Conventions

| | |
|---|---|
| Base URL (via nginx) | `http://localhost:8080` |
| Base URL (API directly, local `go run`) | `http://localhost:8080` |
| Content type | `application/json` on every request and response |
| Auth | `Authorization: Bearer <jwt>`, required on `/patient/search` only |
| Date format | `YYYY-MM-DD` everywhere (never RFC 3339) |
| Correlation id | `X-Request-ID` is echoed on every response; send your own to have it reused |

### Error envelope

Every non-2xx response — including 404s from unknown routes and 500s — uses
exactly this shape. Clients branch on `code`, never on `message`.

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "username, password or hospital is incorrect"
  }
}
```

### Status codes in use

| Status | Meaning |
|--------|---------|
| `200` | success |
| `201` | staff account created |
| `400` | validation error (missing field, bad format, no search criteria) |
| `401` | bad credentials, or missing/malformed/expired token |
| `404` | hospital not found, patient not found, unknown route |
| `405` | wrong method for a known path |
| `409` | username already taken at that hospital |
| `429` | rate limited by nginx (10 req/min on the credential endpoints) |
| `500` | unexpected server error — details are logged, never returned |
| `502` | the hospital's HIS was unreachable or returned garbage |

### Full error-code list

| Code | Status | Where |
|------|--------|-------|
| `VALIDATION_ERROR` | 400 | all endpoints |
| `UNAUTHORIZED` | 401 | `/patient/search` |
| `INVALID_CREDENTIALS` | 401 | `/staff/login` |
| `HOSPITAL_NOT_FOUND` | 404 | `/staff/create` |
| `PATIENT_NOT_FOUND` | 404 | `/patient/search` |
| `ROUTE_NOT_FOUND` | 404 | unknown path |
| `METHOD_NOT_ALLOWED` | 405 | known path, wrong verb |
| `USERNAME_TAKEN` | 409 | `/staff/create` |
| `INTERNAL_ERROR` | 500 | all endpoints |
| `HIS_UNAVAILABLE` | 502 | `/patient/search` |

---

## 1. `POST /staff/create`

Registers a staff account at an existing hospital.

**Auth:** none. See [ADR-0003](adr/0003-staff-create-is-unauthenticated.md) for
why, and what would change in a real deployment.

### Request

```json
{
  "username": "jsmith",
  "password": "P@ssw0rd123",
  "hospital": "hospital-a"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `username` | string | yes | max 64 chars. Unique **within the hospital only** — another hospital may have its own `jsmith` |
| `password` | string | yes | 8–72 characters (72 is bcrypt's limit; longer input is rejected rather than silently truncated). Stored as a bcrypt hash |
| `hospital` | string | yes | the hospital's `code` (`hospital-a`) or its display `name` (`Hospital A`), case-insensitive |

### Response `201 Created`

```json
{
  "id": "3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a",
  "username": "jsmith",
  "hospital": "hospital-a",
  "created_at": "2026-07-30T09:00:00Z"
}
```

The password and its hash never appear in any response.

### Errors

| Status | Code | Scenario |
|---|---|---|
| 400 | `VALIDATION_ERROR` | missing field, password shorter than 8 or longer than 72, malformed JSON |
| 404 | `HOSPITAL_NOT_FOUND` | `hospital` matches no row in `hospitals` |
| 409 | `USERNAME_TAKEN` | that username already exists **at that hospital** |

### Example

```bash
curl -sS -X POST http://localhost:8080/staff/create \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}'
```

---

## 2. `POST /staff/login`

Authenticates a staff member and returns a JWT scoped to their hospital.

**Auth:** none.

`hospital` is required, not decorative: usernames are unique only within a
hospital, so the account lookup is always on the `(hospital, username)` pair.
A lookup by username alone could match a different hospital's employee.

### Request

```json
{
  "username": "jsmith",
  "password": "P@ssw0rd123",
  "hospital": "hospital-a"
}
```

### Response `200 OK`

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

JWT claims (HS256, `JWT_TTL` default 1 hour):

```json
{
  "sub": "3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a",
  "hospital_id": "8a2b1c3d-4e5f-4a7b-8c9d-0e1f2a3b4c5d",
  "username": "jsmith",
  "iss": "hospital-middleware",
  "iat": 1753868400,
  "nbf": 1753868400,
  "exp": 1753872000,
  "jti": "0f2a…"
}
```

### Errors

| Status | Code | Scenario |
|---|---|---|
| 400 | `VALIDATION_ERROR` | a required field is missing |
| 401 | `INVALID_CREDENTIALS` | wrong password, unknown username, unknown hospital, or a username that exists at a *different* hospital |

All four 401 cases return the identical body and take comparable time (an
unknown user is still checked against a dummy bcrypt hash). This is
deliberate: a distinguishable response would let an attacker enumerate which
hospitals and usernames exist.

### Example

```bash
TOKEN=$(curl -sS -X POST http://localhost:8080/staff/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}' \
  | jq -r .access_token)
```

---

## 3. `POST /patient/search`

Searches patients, scoped strictly to the hospital in the caller's token.

**Auth:** required — `Authorization: Bearer <token>`.

`POST` rather than `GET`: the body carries national ids, passport ids and
patient names, and query strings end up in nginx access logs, browser history
and proxy caches. See [ADR-0006](adr/0006-post-for-patient-search.md).

### Request

All fields are optional, but **at least one must be present**; an empty body
would otherwise return the hospital's entire patient roster. Fields present but
blank (`""` or whitespace) are treated as absent.

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

| Field | Type | Matching |
|---|---|---|
| `national_id` | string | exact |
| `passport_id` | string | exact |
| `first_name` | string | case-insensitive substring, against **either** `first_name_th` or `first_name_en` |
| `middle_name` | string | as above |
| `last_name` | string | as above |
| `date_of_birth` | `YYYY-MM-DD` | exact |
| `phone_number` | string | exact |
| `email` | string | case-insensitive exact |

There is no `hospital` field. The scope comes from the signed token; sending
one in the body has no effect.

### Behaviour

1. Query local storage, always filtered by the token's `hospital_id`.
2. If there are matches, return them.
3. If there are none **and** the search included a `national_id` or
   `passport_id`, call that hospital's HIS adapter, store the result
   (`patients` + `hospital_patients`, in one transaction), then re-run the same
   scoped query and return the result.
4. If there are none and the search was by name/phone/email only, return an
   empty list — the HIS exposes no way to search by those fields.

Consequences worth knowing:

- A name-only search that matches nothing is `200 {"results":[],"count":0}`,
  **not** a 404.
- An identifier search that matches nothing locally **or** at the HIS is
  `404 PATIENT_NOT_FOUND`.
- A patient belonging only to another hospital is never returned, on any path.
- Results are capped at 100 rows, ordered by last then first English name.

### Response `200 OK`

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

### Errors

| Status | Code | Scenario |
|---|---|---|
| 400 | `VALIDATION_ERROR` | no search fields supplied, or `date_of_birth` is not `YYYY-MM-DD` |
| 401 | `UNAUTHORIZED` | token missing, malformed, expired, wrongly signed, or carrying no hospital scope |
| 404 | `PATIENT_NOT_FOUND` | identifier search found nothing locally or at the HIS |
| 502 | `HIS_UNAVAILABLE` | HIS timed out, refused the connection, returned 5xx, or returned an unusable body |

### Example

```bash
curl -sS -X POST http://localhost:8080/patient/search \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"national_id":"1234567890123"}'
```

---

## 4. Operational endpoints

| Endpoint | Purpose | Success |
|---|---|---|
| `GET /healthz` | liveness — the process is up | `200 {"status":"ok"}` |
| `GET /readyz` | readiness — the database answers | `200 {"status":"ready"}` / `503` |

Both are used by the container healthchecks in `docker-compose.yml`.

---

## Summary

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/staff/create` | POST | — | register a staff account |
| `/staff/login` | POST | — | authenticate, receive a hospital-scoped JWT |
| `/patient/search` | POST | JWT | search patients within the caller's own hospital |
| `/healthz` | GET | — | liveness probe |
| `/readyz` | GET | — | readiness probe |
