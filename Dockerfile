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

# ---------- runtime stage ----------
FROM alpine:3.20

# ca-certificates for outbound HTTPS to the HIS; tzdata so timestamps are not
# stuck in UTC-only; wget (busybox) backs the container healthcheck.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 appuser

COPY --from=builder /out/api /usr/local/bin/api
COPY --from=builder /out/mockhis /usr/local/bin/mockhis

# Never run the service as root: a container escape should not start as uid 0.
USER appuser

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
    CMD wget --quiet --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/api"]
