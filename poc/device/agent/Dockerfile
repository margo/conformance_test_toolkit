# Stage 1: Build
FROM golang:1.24.3 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o device-agent ./poc/device/agent


# Stage 2: Runtime
FROM scratch

WORKDIR /

COPY --from=builder /app/device-agent .
COPY --from=builder /app/poc/device/agent/config ./config

# Create volume for config override
VOLUME ["/config"]

# Add metadata labels
LABEL description="Margo Device Agent for edge workload management" \
      source="https://github.com/margo/dev-repo" \
      title="Device Agent"

# Use ENTRYPOINT for better flexibility
ENTRYPOINT ["./device-agent"]
CMD ["-config", "config/config.yaml"]
