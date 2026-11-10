# home-apis

Go service providing JSON APIs for home e-ink panels: iCloud calendar events and solar data.

## Development

```sh
go vet ./... && go test ./... && test -z "$(gofmt -l .)"
cp deploy/config.example.json config.local.json   # gitignored; add your secrets
go run ./cmd/home-apis -c config.local.json
```

## Endpoints

| Route | Description |
|---|---|
| `GET /health` | liveness, never requires auth |
| `GET /events` | today/tomorrow calendar events |
| `GET /kitchen` | kitchen panel payload (calendar + solar) |
| `GET /solar` | latest solar snapshot |

If `api_token` is set, all routes except `/health` need `Authorization: Bearer <token>`.

## Config

JSON file via `--config`/`-c`; env vars override. See `deploy/config.example.json`.

| JSON key | Env var | Default |
|---|---|---|
| `icloud_username` | `ICLOUD_USERNAME` | required |
| `icloud_app_password` | `ICLOUD_APP_PASSWORD` | required |
| `calendar_name` | `CALENDAR_NAME` | `Family` |
| `port` | `PORT` | `3000` |
| `max_events` | `MAX_EVENTS` | `3` |
| `kitchen_max_events` | `KITCHEN_MAX_EVENTS` | `5` |
| `cache_ttl_seconds` | `CACHE_TTL_SECONDS` | `300` |
| `api_token` | `API_TOKEN` | empty (auth off) |
| `timezone` | `TIMEZONE` | `Europe/Zurich` |
| `solar_source` | `SOLAR_SOURCE` | `fake` |
| `solar_poll_seconds` | `SOLAR_POLL_SECONDS` | `60` |

## Install on the LXC

```sh
curl -fsSL https://github.com/githendrik/home-apis/releases/latest/download/install.sh | sh
```

Then edit `/opt/home-apis/config.json`; logs via `journalctl -u home-apis -f`.
Release: push a `v*` tag; CI publishes the binary, checksum and `install.sh`.

See [docs/architecture.md](docs/architecture.md) and [AGENTS.md](AGENTS.md).
