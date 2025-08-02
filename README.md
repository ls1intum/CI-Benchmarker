# CI-Benchmarker

## Architecture

![Architecture](./docs/img/benchmark-system-architecture.png)

## Usage

```bash
  docker-compose up
```

Use the [bruno](https://www.usebruno.com/) examples in the `docs` folder to test the system

## Development

Start in dev mode

```bash
  DEBUG=True go run .
```

### Generate DB code

- Update the `sqlc.yaml` file with the correct connection string
- Update the `schema.sql` file with the correct schema
- Update the `queries.sql` file with the correct queries

```bash
  sqlc generate
```

Make sure the generated types still work ;)

# Deployment on a VM with publich IP
Firts run
```bash
    chmod 600 traefik/acme.json
```

Then run 
```bash
  docker compose up
```
