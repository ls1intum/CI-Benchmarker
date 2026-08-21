FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Stamped into the binary and into every benchmark_run row, so a running
# deployment can state which build it is and exported data records which build
# produced it. The CI workflow passes the commit SHA.
ARG VERSION=dev

ENV CGO_ENABLED=1

RUN go build \
    -ldflags "-X github.com/Hades-Scheduler/CI-Benchmarker/shared/config.Version=${VERSION}" \
    -o /out/benchmarker .

FROM alpine

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/benchmarker /app/benchmarker

# The database lives on a volume; see docker-compose.yml. Defaulting DB_PATH
# here means a bare `docker run` without a mount still works, but writes into
# the container filesystem.
ENV DB_PATH=/data/benchmark.db
VOLUME /data

CMD ["/app/benchmarker"]
