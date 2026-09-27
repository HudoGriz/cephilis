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
# NOTE: cephilis reads CephFS xattrs and the host's /proc/self/mountinfo and
# ceph debugfs, so it must see the host's CephFS mount. Install the binary or
# package on the host; see README.md.
FROM scratch
COPY --from=builder /bin/cephilis /cephilis
ENTRYPOINT ["/cephilis"]
