# AGENTS.md — home-apis

Small Go service on the home Proxmox host that feeds the e-ink panels:

- `GET /events` — iCloud family calendar, today + tomorrow. Drop-in replacement
  for the old Deno `icloud-calendar-api` (same JSON contract) read by the
  desk panels.
- `GET /kitchen` — calendar + solar + today's production/consumption history
  in one compact response for the battery-powered kitchen panel
  ([eink-kitchen-panel](https://github.com/githendrik/eink-kitchen-panel)),
  which draws the history as a chart.
- `GET /solar` — latest solar snapshot (`fake` data, or the GoodWe SEMS+ cloud with `solar_source: "sems"`, which is what CT 110 runs).
- `GET /health` — public liveness + calendar/solar/history status.

## Read in this order

1. [`docs/status.md`](docs/status.md) — what's deployed, what's open (incl. the pending cut-over).
2. [`docs/decisions.md`](docs/decisions.md) — why Go, why one service, why SEMS+ cloud (inverter on guest WiFi), why the history looks the way it does.
3. [`docs/architecture.md`](docs/architecture.md) — contracts (incl. `/kitchen` `history`), caching, CalDAV details, the SEMS+ API we use, deployment, operations.

`docs/superpowers/specs|plans/` are the original design and plan (2026-11-10),
historical. The docs above win where they differ. The history chart's design
and plan live in the panel repo (`eink-kitchen-panel/docs/superpowers/`,
2026-11-26).

## Repository map

| Path | What |
|---|---|
| `cmd/home-apis/` | `main.go`: wiring, `--version`, hidden `--dump-events` / `--dump-solar` debug flags |
| `internal/config/` | JSON file + env config (Deno-compatible keys) |
| `internal/calendar/` | iCloud CalDAV fetcher (`caldav.go`), RRULE expansion (`expand.go`), day grouping + `/events` encoding (`response.go`), cache with stale-while-revalidate (`service.go`) |
| `internal/solar/` | `Source` interface, `fake` source, SEMS+ cloud source (`sems.go`: login, signature, flow + day curve), `History` + 5-minute binning (`history.go`), poller/cache incl. the history schedule (`poller.go`) |
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
/opt/home-apis/home-apis -c /opt/home-apis/config.json --dump-solar    # live solar check: SEMS+ login, one flow read, history summary
```

Live SEMS check from the Mac without putting credentials anywhere: store the
visitor account once in the Keychain (`security add-generic-password -s home-apis-sems -a '<email>' -w`),
then run `SEMS_USERNAME="$(security find-generic-password -s home-apis-sems | sed -n 's/.*"acct"<blob>="\(.*\)"/\1/p')" SEMS_PASSWORD="$(security find-generic-password -s home-apis-sems -w)" ICLOUD_USERNAME=x ICLOUD_APP_PASSWORD=x SOLAR_SOURCE=sems go run ./cmd/home-apis --dump-solar`.
To put the credentials into CT 110's config, pipe them from the Keychain over
SSH stdin into a script running in the container. Never pass them as arguments
or echo them.

## Rules

- Keep `/events` byte-compatible with the Deno service (keys, order, 2-space indent, labels "Today"/"Tomorrow", `fetched_at` with ms) — other panels parse it. Intentional differences are listed in `docs/decisions.md`.
- `/kitchen` must answer from cache (< 1 s); never block on iCloud or GoodWe after the first fetch.
- `/kitchen` must stay ≤ 4000 bytes on a busy day: the panel keeps the last body in RTC memory for offline re-renders (`TestKitchenRealisticBodyFitsPanel`, about 3.4 KB). Any new field there needs that budget checked. That's why `/kitchen` is compact JSON.
- The `history` contract (bins, units, `null`, only for today and `shift=0`) is parsed by the panel firmware. Change it only together with `eink-kitchen-panel/lib/core/src/kitchen.cpp`.
- Repo is public: never commit credentials. Real config lives only in `/opt/home-apis/config.json` on the LXC.
- Update `docs/architecture.md` when behaviour changes, `docs/decisions.md` for non-obvious choices, `docs/status.md` when the deployment changes.
- Homelab changes follow the owner's homelab process (backup, verify, daily log in Obsidian `Personal/Homelab/daily-log/`).

## Gotchas

- **iCloud rejects partial calendar-data requests**: go-webdav's `QueryCalendar` always sends `<c:comp …>`, and iCloud answers with empty `VCALENDAR` shells. The REPORT is therefore sent by our own `reportCalendar` with plain `<c:calendar-data/>`. Unit tests can't catch this — verify against live iCloud with `--dump-events`.
- **iCloud serves the calendar home from a shard host** (`pNN-caldav.icloud.com`); go-webdav drops the host, so the raw PROPFIND is inspected (`hrefRecorder`).
- **The Mac's corporate proxy** (`127.0.0.1:3128`) returns 502 for LAN addresses: use `curl --noproxy '*'`.
- **SEMS+ accounts are regional.** The visitor account lives on the EU server: the global `semsplus.goodwe.com` login answers `100005` "Email or password error" even with the right password. We log in at `eu-semsplus.goodwe.com` and use the `api` gateway the login returns.
- **SEMS+ signs are the opposite of ours:** `pGrid` > 0 = export and `pBat` > 0 = discharge; `snapshot()` flips both. Values are kW. The flow updates on 5-minute marks; the day curve has one point per minute.
- **Repeated failed SEMS logins risk an account lockout.** The source waits 15 min after a rejected login; don't loop logins by hand either.
- **On the work VPN the corporate proxy decrypts TLS** (GoodWe certificates are issued by "Swisscom Managed Gin Proxy"), so SEMS credentials pass through it. Only test with the read-only visitor account, preferably off VPN. `ssh lab` is refused while on VPN.
- **The SEMS+ API is undocumented.** If it breaks, compare with [goodwe-sems-home-assistant](https://github.com/TimSoethout/goodwe-sems-home-assistant) (`custom_components/sems/sems_api.py`) and the EU web app's JS bundles (`eu-semsplus.goodwe.com/config.js`, `assets/js/*`), which is how the login, signature and day-curve endpoint were found.
