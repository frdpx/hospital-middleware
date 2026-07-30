# ADR-0004: The hospital scope lives in the JWT, not in the request

**Status:** accepted

**Context.** `/patient/search` must return only patients of the caller's own
hospital. The naive design takes a `hospital` field in the search body.

**Decision.** `hospital_id` is a claim in the access token, set at login from
the staff row and signed. `POST /patient/search` has no hospital field at all;
the handler reads `claims.HospitalID` and the service passes it into the SQL
`WHERE` clause. Tokens are HS256 and the parser pins the accepted algorithm.

**Consequences.**
- A client cannot widen its own scope: forging a different `hospital_id`
  requires the signing key. A `hospital` field in the body is simply ignored
  (there is a test asserting exactly this).
- The scope is fixed for the token's lifetime (default 1 hour), so revoking a
  staff member's hospital access is not instant. Acceptable at a 1-hour TTL.
- Pinning `alg` to HS256 blocks the classic `alg: none` forgery, and would
  block key-confusion if we later moved to RS256.
- `hospital_id` appears in the access log for every authenticated request,
  which makes "who looked at what" auditable.

**Revisit when.** Staff can belong to more than one hospital — the claim
becomes a list and the SQL filter becomes `= ANY($1)`.
