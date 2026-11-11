# AGENTS.md — home-apis

Small Go service on the home Proxmox host that feeds the e-ink panels:

- `GET /events` — iCloud family calendar, today + tomorrow. Drop-in replacement
  for the old Deno `icloud-calendar-api` (same JSON contract) read by the
  desk panels.
- `GET /kitchen` — calendar + solar in one response for the battery-powered
  kitchen panel ([eink-kitchen-panel](https://github.com/githendrik/eink-kitchen-panel)).
- `GET /solar` — latest solar snapshot (currently **fake data**; GoodWe not installed yet).
- `GET /health` — public liveness + source status.

## Read in this order

1. [`docs/status.md`](docs/status.md) — what's deployed, what's open (incl. the pending cut-over).
2. [`docs/decisions.md`](docs/decisions.md) — why Go, why one service, why the solar source is undecided.
3. [`docs/architecture.md`](docs/architecture.md) — contracts, caching, CalDAV details, deployment, operations.

`docs/superpowers/specs|plans/` are the original design and plan (2026-11-10),
historical. The docs above win where they differ.

## Repository map

| Path | What |
|---|---|
| `cmd/home-apis/` | `main.go`: wiring, `--version`, hidden `--dump-events` debug flag |
| `internal/config/` | JSON file + env config (Deno-compatible keys) |
| `internal/calendar/` | iCloud CalDAV fetcher (`caldav.go`), RRULE expansion (`expand.go`), day grouping + `/events` encoding (`response.go`), cache with stale-while-revalidate (`service.go`) |
| `internal/solar/` | `Source` interface, `fake` source, poller/cache |
| `internal/server/` | HTTP handlers + bearer auth |
| `deploy/` | systemd unit, example config |
| `install.sh` | installer/upgrader used on the LXC (published with each release) |

## Commands

```sh
go vet ./... && go test -race ./... && test -z "$(gofmt -l .)"   # CI runs the same
go run ./cmd/home-apis -c config.local.json                      # config.local.json is gitignored
git tag vX.Y.Z && git push origin vX.Y.Z                         # release (binary + install.sh)
# on the LXC (CT 110 on host `lab`), as root:
curl -fsSL https://github.com/githendrik/home-apis/releases/latest/download/install.sh | sh
/opt/home-apis/home-apis -c /opt/home-apis/config.json --dump-events   # live iCloud check with debug log
```

## Rules

- Keep `/events` byte-compatible with the Deno service (keys, order, 2-space indent, labels "Today"/"Tomorrow", `fetched_at` with ms) — other panels parse it. Intentional differences are listed in `docs/decisions.md`.
- `/kitchen` must answer from cache (< 1 s); never block on iCloud or GoodWe after the first fetch.
- Repo is public: never commit credentials. Real config lives only in `/opt/home-apis/config.json` on the LXC.
- Update `docs/architecture.md` when behaviour changes, `docs/decisions.md` for non-obvious choices, `docs/status.md` when the deployment changes.
- Homelab changes follow the owner's homelab process (backup, verify, daily log in Obsidian `Personal/Homelab/daily-log/`).

## Gotchas

- **iCloud rejects partial calendar-data requests**: go-webdav's `QueryCalendar` always sends `<c:comp …>`, and iCloud answers with empty `VCALENDAR` shells. The REPORT is therefore sent by our own `reportCalendar` with plain `<c:calendar-data/>`. Unit tests can't catch this — verify against live iCloud with `--dump-events`.
- **iCloud serves the calendar home from a shard host** (`pNN-caldav.icloud.com`); go-webdav drops the host, so the raw PROPFIND is inspected (`hrefRecorder`).
- **The Mac's corporate proxy** (`127.0.0.1:3128`) returns 502 for LAN addresses: use `curl --noproxy '*'`.
