# ADR-0003: `/staff/create` is left unauthenticated

**Status:** accepted (with a stated caveat)

**Context.** The assignment lists `/staff/create` with inputs
`username, password, hospital` and says nothing about who may call it. A
reviewer needs to be able to create an account and then log in, with no
bootstrap step.

**Decision.** Ship it unauthenticated, but constrain it: the `hospital` must
already exist (hospitals are operator-provisioned reference data, not
API-creatable), passwords are 8–72 characters and bcrypt-hashed, and nginx
rate-limits the endpoint to 10 requests/minute per IP.

**Consequences.**
- The API is demonstrable end to end from a clean database with two curl calls.
- We accept that anyone who can reach the service can mint a staff account —
  and therefore read that hospital's patient data. **This is not acceptable in
  production** and is called out here rather than hidden.

**What production would do instead.** Require an admin JWT (a `role` claim on
the existing token), or move staff provisioning out of the public API into an
internal admin surface. Either is a small change: the handler already receives
the router's middleware chain, so it is one `RequireAuth`-style guard plus a
role check.

**Revisit when.** This service is exposed beyond a trusted network — before
that, not after.
