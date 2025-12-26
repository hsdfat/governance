FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go.mod and go.sum from governance root
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the telco manager
WORKDIR /build/examples/telco_manager
RUN CGO_ENABLED=0 GOOS=linux go build -o telco-manager main.go

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/examples/telco_manager/telco-manager .

# Expose HTTP port
EXPOSE 8080

# Run the telco manager
ENTRYPOINT ["./telco-manager"]
