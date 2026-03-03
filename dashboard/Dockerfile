FROM golang:1.25 AS builder

WORKDIR /workspace

# Copy go mod first (cache layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy full project
COPY api/ api/
COPY dashboard/ dashboard/

# Build dashboard binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -a -o dashboard-bin ./dashboard

# -------------------------
# Runtime
# -------------------------
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/dashboard-bin /dashboard

USER 65532:65532

ENTRYPOINT ["/dashboard"]