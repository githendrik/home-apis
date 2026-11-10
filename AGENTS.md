# AGENTS.md

- Build: `go build ./cmd/home-apis`
- Test: `go vet ./... && go test ./... && test -z "$(gofmt -l .)"` (CI runs the same)
- Run: `go run ./cmd/home-apis -c config.local.json` (`config.local.json` is gitignored; copy `deploy/config.example.json`)
- Release: _not yet defined_
- The repo is public: never commit real credentials.
- Update `docs/architecture.md` whenever behaviour changes.

See [docs/architecture.md](docs/architecture.md).
