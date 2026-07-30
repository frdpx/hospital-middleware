# Architecture Decision Records

Each record states the constraints that forced a choice, the choice, what we
gain, what we accept, and the signal that would make us revisit it.

| # | Decision |
|---|---|
| [0001](0001-layered-monolith-with-consumer-side-interfaces.md) | A layered monolith with consumer-side interfaces |
| [0002](0002-split-patients-from-hospital-patients.md) | Split `patients` from `hospital_patients` |
| [0003](0003-staff-create-is-unauthenticated.md) | `/staff/create` is left unauthenticated (with a stated caveat) |
| [0004](0004-hospital-scope-lives-in-the-jwt.md) | The hospital scope lives in the JWT, not in the request |
| [0005](0005-embedded-migrations-on-startup.md) | Migrations are embedded and applied on API startup |
| [0006](0006-post-for-patient-search.md) | `/patient/search` is POST, not GET |
| [0007](0007-his-fallback-only-for-identifier-searches.md) | Local-first search; the HIS is only consulted for identifier lookups |
