FROM golang:1.25.10 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/jumi ./cmd/jumi
# Runtime-side helper ownership lives in the separate node-artifact-runtime
# repository. JUMI only consumes the published nan artifact.
RUN GOBIN=/out CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go install github.com/HeaInSeo/node-artifact-runtime/cmd/node-artifact-runtime@bee9f999c242180f3f447591b54ab511a8413ae5

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/jumi /usr/local/bin/jumi
COPY --from=builder /out/node-artifact-runtime /usr/local/bin/nan
# Transitional copy only:
# the runtime-side helper belongs to the DAG node runtime image contract, not
# to the JUMI service image contract. Keep only the canonical `nan` path in
# the service image while the smoke fixture still uses the JUMI image as a
# node runtime shortcut.

EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/jumi"]
