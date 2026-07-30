# ADR-0007: Local-first search; the HIS is only consulted for identifier lookups

**Status:** accepted

**Context.** `/patient/search` accepts eight optional filters, but the Hospital
A HIS exposes exactly one lookup: `GET /patient/search/{id}` where `id` is a
national id or a passport id. There is no name search upstream.

**Decision.**

1. Always query local storage first, scoped to the token's hospital.
2. On a miss, call the HIS **only** if the search included `national_id` or
   `passport_id`; upsert the result, then re-run the same scoped query.
3. A name/phone/email miss returns `200 {"results":[],"count":0}`.
4. An identifier miss both locally and at the HIS returns `404
   PATIENT_NOT_FOUND`.

**Why re-run the query instead of returning the HIS payload.** The HIS answers
on the identifier alone. If the caller also sent `last_name: "Wrongname"`,
returning the fetched patient would hand back someone they did not ask for.
Re-running the scoped query keeps the hospital filter *and* every other
criterion authoritative on one code path. There is a test for exactly this.

**Consequences.**
- A name search never blocks on an external call — it cannot, since there is
  nothing to call.
- The empty-list vs 404 distinction is asymmetric and needs documenting; it is
  in `docs/api-spec.md`.
- HIS failures are separated: `ErrPatientNotFound` → 404, everything else
  (timeout, 5xx, malformed body) → 502 `HIS_UNAVAILABLE`, so a client can tell
  "no such patient" from "try again later".
- A cold database means the first search for each patient pays one HIS
  round-trip. `HIS_TIMEOUT` (default 5s) bounds it.

**Revisit when.** A HIS exposes richer search, or `synced_at` should trigger a
refresh of stale-but-present records rather than only filling absences.
