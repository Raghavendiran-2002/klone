FROM golang:1.25 as builder

WORKDIR /workspace

# Copy the api module
COPY api/ api/

# Copy dashboard code
COPY dashboard/main.go dashboard/

# Build dashboard
RUN cd dashboard && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -o dashboard main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/dashboard/dashboard .
USER 65532:65532

ENTRYPOINT ["/dashboard"]
