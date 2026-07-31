FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mockhis ./cmd/mockhis

FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 appuser

USER appuser

FROM runtime AS mockhis

COPY --from=builder /out/mockhis /usr/local/bin/mockhis

EXPOSE 9090
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
    CMD wget --quiet --spider http://127.0.0.1:9090/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/mockhis"]

FROM runtime AS api

COPY --from=builder /out/api /usr/local/bin/api

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
    CMD wget --quiet --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/api"]
