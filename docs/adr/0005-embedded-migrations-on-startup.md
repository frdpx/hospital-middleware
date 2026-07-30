# ADR-0005: Migrations are embedded and applied on API startup

**Status:** accepted

**Context.** The schema must exist before the first request. The assignment
asks for a compose stack of nginx + Go + Postgres, and leaves the migration
strategy to us.

**Options considered.**

| Option | Why not |
|---|---|
| `psql` init scripts in `/docker-entrypoint-initdb.d` | Only runs on a *fresh* volume; no versioning, no rollback, no way to ship change #2 |
| A separate `migrate` container/job in compose | Correct for production, but adds a fourth service and an ordering dance to a three-service brief |
| ORM auto-migrate | Implicit, unreviewable, and generates DDL nobody wrote |

**Decision.** Numbered SQL files in `migrations/`, embedded into the binary via
`go:embed`, applied by `golang-migrate` at startup when `DB_AUTO_MIGRATE=true`.

**Consequences.**
- The image is self-contained: the code and the schema it expects ship
  together, so a container can never run SQL from a different build.
- golang-migrate takes a Postgres advisory lock, so several replicas starting
  simultaneously still apply each migration exactly once.
- Plain SQL stays reviewable in a pull request, and every migration has a
  `.down.sql`.
- We accept that the API process needs DDL privileges on its database.
- `DB_AUTO_MIGRATE=false` is the escape hatch for environments that require a
  human-gated schema change; migrations then run as a separate step.

**Revisit when.** A migration becomes long-running (large table rewrite) —
those should be gated and run out-of-band rather than blocking a deploy.
