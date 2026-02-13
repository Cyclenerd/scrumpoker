# Build stage
FROM docker.io/library/golang:1.25-alpine AS builder

WORKDIR /app

# Copy source code
COPY go.mod go.sum main.go ./
COPY store ./store
RUN go mod download

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o scrumpoker .

# Runtime stage
FROM scratch

WORKDIR /root

# Copy binary from builder
COPY --from=builder /app/scrumpoker .

# Copy static assets
COPY templates ./templates
COPY static ./static

# Expose port
EXPOSE 8080

# Add container metadata
LABEL org.opencontainers.image.authors="Cyclenerd"

# Run the application
CMD ["./scrumpoker"]
