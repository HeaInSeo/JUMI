FROM golang:1.25.10 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/jumi ./cmd/jumi
# Transitional build only:
# keep producing the runtime-side helper from the in-repo compatibility source
# until nan is fully split out of JUMI.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/nan ./cmd/jumi-output-helper

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/jumi /usr/local/bin/jumi
COPY --from=builder /out/nan /usr/local/bin/nan
# Transitional copy only:
# the runtime-side helper belongs to the DAG node runtime image contract, not
# to the JUMI service image contract. Keep only the canonical `nan` path in
# the service image while the smoke fixture still uses the JUMI image as a
# node runtime shortcut.

EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/jumi"]
