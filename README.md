# CI-Benchmarker

## Architecture

![Architecture](./docs/img/benchmark-system-architecture.png)

## Usage

```bash
cp .env.example .env      # set BENCHMARKER_HOST and BENCHMARKER_IMAGE_TAG
docker compose up -d
```

The database lives in the `benchmark-data` volume rather than in the container
filesystem, so `--force-recreate` cannot destroy a collected dataset. The
compose file runs a pinned image; there is deliberately no `build: .`, so a
redeploy cannot silently swap in whatever is in the working tree.
`BENCHMARKER_IMAGE_TAG` has no default and compose refuses to start without it,
so a campaign cannot accidentally run a moving `latest`.

Use the [Bruno](https://www.usebruno.com/) examples in [`bruno/`](./bruno) to test the system

## Development

Start in dev mode

```bash
  DEBUG=true go run .
```

### Checks

These are exactly what CI runs; the image build is gated on them passing.

```bash
unformatted=$(gofmt -l .)
test -z "$unformatted"
go vet ./...
CGO_ENABLED=1 go test -race -timeout 15m ./...
```

### Generate Open API spec
Install swag by using:
```bash
  go install github.com/swaggo/swag/cmd/swag@latest
```

Run swag init in the project's root folder which contains the main.go file. This will parse your comments and generate the required files (docs folder and docs/docs.go).

```bash
  swag init
```

### Generate DB code

- Update the `sqlc.yaml` file with the correct connection string
- Update the `schema.sql` file with the correct schema
- Update the `queries.sql` file with the correct queries

```bash
  sqlc generate
```

Make sure the generated types still work ;)

# Deployment on a VM with public IP
First run
```bash
  touch traefik/acme.json
  chmod 600 traefik/acme.json
```

Then run 
```bash
  docker compose up
```
