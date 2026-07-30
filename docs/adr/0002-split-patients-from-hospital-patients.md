# ADR-0002: Split `patients` from `hospital_patients`

**Status:** accepted

**Context.** The same person can be a patient at several hospitals, and each
hospital's HIS assigns its own local `patient_hn`. Meanwhile the hardest
requirement in the assignment is that staff must only ever see their own
hospital's patients.

**Decision.** Two tables. `patients` holds the canonical person, keyed by the
identifiers that are unique across hospitals (`national_id`, `passport_id`).
`hospital_patients` links a person to a hospital and carries that hospital's
`patient_hn` and `synced_at`.

**Consequences.**
- One person, one demographic record, however many hospitals know them.
- Access control becomes a schema property: every patient query starts
  `FROM hospital_patients WHERE hospital_id = $1`, so another hospital's rows
  are unreachable rather than merely un-selected.
- The HIS upsert must touch two tables, so it runs in a transaction — a
  `patients` row with no link would be invisible to every search.
- Uniqueness needs *partial* indexes (identifiers are nullable), which is why
  the upsert is find-then-write instead of a single `ON CONFLICT`.

**Revisit when.** A hospital needs several registrations for one person (e.g.
per-branch HNs) — then `hospital_patients` gains a branch column and its
`(hospital_id, patient_id)` unique index widens.
