# Multi-stage build for smaller production image
FROM golang:1.21-alpine AS builder

# Set working directory
WORKDIR /app

# Install git for go modules
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o taskforge-example ./examples/basic/

# Production stage
FROM alpine:latest

# Add ca-certificates for HTTPS calls
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/taskforge-example .

# Expose port (if your example includes HTTP server)
EXPOSE 8080

# Command to run
CMD ["./taskforge-example"]