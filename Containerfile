FROM golang:1.25.10 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/jumi ./cmd/jumi
# Compatibility build only:
# keep producing the legacy runtime helper binary for smoke/dev shortcuts until
# the node runtime base image and explicit helper split are introduced.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/jumi-output-helper ./cmd/jumi-output-helper

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/jumi /usr/local/bin/jumi
COPY --from=builder /out/jumi-output-helper /usr/local/bin/nan
# Compatibility copy only:
# the runtime-side helper belongs to the DAG node runtime image contract, not
# to the JUMI service image contract. This copy remains for current smoke/dev
# paths that still use the JUMI image as a node runtime shortcut.
RUN ln -s /usr/local/bin/nan /usr/local/bin/jumi-output-helper

EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/jumi"]
