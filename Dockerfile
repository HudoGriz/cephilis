# Stage 1: build
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X github.com/HudoGriz/cephilis/internal/cli.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
      -o /bin/cephilis ./cmd/cephilis

# Stage 2: minimal runtime
# NOTE: cephilis shells out to mountpoint, stat, and du from the host — the
# container is only used for building/distributing the binary.  For actual
# HPC deployment install the binary directly; see README.md.
FROM scratch
COPY --from=builder /bin/cephilis /cephilis
ENTRYPOINT ["/cephilis"]
