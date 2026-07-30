# ---------- build stage ----------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Dependencies are copied and downloaded first so this layer stays cached
# across source-only changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary that runs on a bare image.
# -trimpath strips local paths; -s -w drop the symbol table and DWARF data.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mockhis ./cmd/mockhis

# ---------- shared runtime base ----------
FROM alpine:3.20 AS runtime

# ca-certificates for outbound HTTPS to the HIS; tzdata so timestamps are not
# stuck in UTC-only; wget (busybox) backs the container healthcheck.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 appuser

# Never run as root: a container escape should not start as uid 0.
USER appuser

# ---------- mock HIS (development only, `--target mockhis`) ----------
# Built as its own image so the development stand-in is not present in — and
# cannot be started from — the image that runs in production.
FROM runtime AS mockhis

COPY --from=builder /out/mockhis /usr/local/bin/mockhis

EXPOSE 9090
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
    CMD wget --quiet --spider http://127.0.0.1:9090/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/mockhis"]

# ---------- API (default target) ----------
FROM runtime AS api

COPY --from=builder /out/api /usr/local/bin/api

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
    CMD wget --quiet --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/api"]
