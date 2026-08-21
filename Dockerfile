FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Dependencies in their own layer so a source-only change does not re-download
# the whole module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1

RUN go build -o /out/benchmarker .

FROM alpine

# Needed to reach systems under test over HTTPS and to interpret timestamps.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy only the binary. The previous image copied the entire build context,
# including sources and the .git-derived build tree.
COPY --from=builder /out/benchmarker /app/benchmarker

# The database lives on a volume; see docker-compose.yml.
ENV DB_PATH=/data/benchmark.db
VOLUME /data

CMD ["/app/benchmarker"]
