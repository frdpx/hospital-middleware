# ADR-0001: A layered monolith with consumer-side interfaces

**Status:** accepted

**Context.** Three endpoints, one team, a three-day budget, and an evaluation
that weights readability and test coverage. The system integrates with N
external HIS APIs whose shapes we do not control.

**Decision.** One Go binary, layered `handler → service → repository`, with the
narrow interfaces (`StaffRepository`, `PatientRepository`, `HISClient`,
`Factory`) declared in the *consuming* package rather than beside the
implementation. `cmd/api/main.go` is the only file that names concrete types.

**Consequences.**
- Every business rule is unit-testable with in-memory fakes; the suite runs in
  seconds with no database and no Docker.
- Each interface lists only the methods its consumer uses, so repository
  changes do not ripple through unused mock methods.
- We accept the usual monolith trade-off: all three endpoints deploy together.
  With this call volume that is a feature, not a cost.

**Revisit when.** Patient search needs to scale independently of staff auth, or
a second team owns the HIS integrations — then `hisclient` is the natural first
service to extract, since it already sits behind an interface.
