# ADR-0006: `/patient/search` is POST, not GET

**Status:** accepted

**Context.** A search is semantically a read, which argues for `GET` with query
parameters. But its parameters here are national ids, passport ids, patient
names, dates of birth, phone numbers and emails.

**Decision.** `POST /patient/search` with a JSON body.

**Consequences.**
- Identifiers stay out of URLs, and therefore out of nginx access logs, browser
  history, `Referer` headers and any intermediate proxy cache. With `GET` we
  would be writing national ids into `access.log` on every search.
- The typed JSON body handles optional fields cleanly: a pointer field
  distinguishes "not filtering on this" from "match the empty value", which is
  awkward with query strings.
- We give up HTTP caching and idempotency semantics. Neither is useful here —
  results change as the HIS is synced, and we would not want them cached.
- We accept the mild REST impurity, and document it rather than hiding it.

**Note.** The service also logs no request bodies and no query strings, for the
same reason.

**Revisit when.** A client genuinely needs cacheable, bookmarkable searches —
then a `GET` variant restricted to non-identifying fields could be added
alongside.
