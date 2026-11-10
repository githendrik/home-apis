# home-apis

Go service providing JSON APIs for home e-ink panels: iCloud calendar events and solar data.

## Development

```sh
go vet ./... && go test ./... && test -z "$(gofmt -l .)"
cp deploy/config.example.json config.local.json   # gitignored; add your secrets
go run ./cmd/home-apis -c config.local.json
```

See [docs/architecture.md](docs/architecture.md) and [AGENTS.md](AGENTS.md).
