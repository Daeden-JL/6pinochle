# Build stage
FROM golang:1.21.5-alpine AS builder

WORKDIR /app

# Copy go module dependency files and download
COPY go.mod go.sum ./
RUN go mod download

# Copy the remaining project source code (including templates)
COPY . .

# Build the Go application binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server .

# Run stage
FROM alpine:3.19

# Install standard CA certificates and time-zone data
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the compiled executable from the build stage
COPY --from=builder /app/server .

# Expose port 8080
EXPOSE 8080

# Configure environment variables
ENV PORT=8080

# Execute the application
CMD ["./server"]
