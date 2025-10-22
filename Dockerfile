# Use an official Go runtime as a parent image
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache gcc musl-dev

# Set the working directory inside the container
WORKDIR /app

# Copy files and download dependencies
COPY . . 
RUN go mod download


# Build the Go application
ENV CGO_ENABLED=1

RUN go build -o benchmarker .

# Start a new stage for the runtime container
FROM alpine

# Set the working directory inside the minimal runtime container
WORKDIR /app

# Copy the built binary from the builder container into the minimal runtime container
COPY --from=builder /app . 

# Run your Go application
CMD ["/app/benchmarker"]