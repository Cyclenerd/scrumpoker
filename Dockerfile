# Build stage
FROM docker.io/library/golang:1.25-alpine AS builder

WORKDIR /app

# Copy source code
COPY go.mod go.sum main.go ./
COPY store ./store
RUN go mod download

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o scrumpoker .

# Runtime stage
FROM docker.io/library/alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/scrumpoker .

# Copy static assets
COPY templates ./templates
COPY static ./static

# Expose port
EXPOSE 8080

# Set environment variable (override at runtime)
ENV PORT=8080

# Run the application
CMD ["./scrumpoker"]
