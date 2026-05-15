# dansal

Calendar REST API backed by SQLite. See [API.md](API.md) for full endpoint documentation.

## Requirements

- Go 1.22+
- SQLite3

## Run

```bash
go run .
```

The server starts on the port set in `config.yaml` (default `8000`). On first run an admin user is created and the password is printed to the console.

## Configuration

`config.yaml`:

```yaml
server:
  port: 8000
```

## Build

```bash
go build -o dansal
./dansal
```
