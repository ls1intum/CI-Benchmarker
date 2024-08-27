# CI-Benchmarker

## Usage

TODO

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
