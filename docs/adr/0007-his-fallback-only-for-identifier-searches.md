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

**Why "no local match" is not enough to justify calling the HIS.** The first
implementation triggered the fallback whenever the *full* filtered search
returned zero rows. That made a mismatched extra criterion
(`national_id` we hold + `last_name` that does not match) call the HIS and
write a transaction on **every** request, forever, for a guaranteed 404 —
measured at one upstream call per request against the running stack. The
service now asks a second, cheap question first: *is this identifier already on
file for this hospital?* If it is, the miss came from the other criteria and no
amount of re-fetching will change the answer. Only a genuinely unknown
identifier reaches the HIS.

**Why the sync merges rather than overwrites.** `patients` is shared across
hospitals, so a re-sync driven by Hospital B must not erase what Hospital A's
HIS supplied for the same person. `repository.updatePatient` COALESCEs every
field, meaning an absent incoming value leaves the stored one intact. The cost
is that this path can never *clear* a field; deliberate deletion is an operator
action, not a side effect of somebody running a search.

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
