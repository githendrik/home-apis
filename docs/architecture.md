# Architecture

## Overview

`home-apis` is a small Go service that serves JSON for e-ink panels: iCloud
calendar events (port of the Deno `icloud-calendar-api`) and solar data.

## Components

- `cmd/home-apis` – entry point; loads config (tzdata embedded for minimal containers).
- `internal/config` – config loading (JSON file + env overrides).
- `internal/server` – HTTP handler (`/health`, `/events`, `/solar`, `/kitchen`).

## Endpoints

### `/events` contract

Produced by `calendar.Build` + `calendar.Encode` (2-space indented JSON, no
trailing newline, `&` etc. not HTML-escaped). `today` is today + shift (shift
is applied by the caller, e.g. for the late-evening kitchen panel); labels stay
`"Today"` / `"Tomorrow"`. Per day: all-day events first, then by start, then
truncated to `max_events`. An all-day event appears on every day in
`[start, end)` (end exclusive); a timed event only on its start day. Timed
`start`/`end` are local wall time `YYYY-MM-DDTHH:MM:SS` (no zone); all-day are
`YYYY-MM-DD`. `fetched_at` is UTC with milliseconds. Empty days give `[]`.

```json
{
  "today": {
    "label": "Today",
    "date": "2026-06-05",
    "events": [
      {
        "summary": "Dentist",
        "start": "2026-06-05T14:00:00",
        "end": "2026-06-05T15:00:00",
        "allDay": false
      }
    ]
  },
  "tomorrow": {
    "label": "Tomorrow",
    "date": "2026-06-06",
    "events": [
      {
        "summary": "Hochzeitstag",
        "start": "2026-06-06",
        "end": "2026-06-07",
        "allDay": true
      }
    ]
  },
  "fetched_at": "2026-06-05T08:00:00.000Z"
}
```

### HTTP endpoints (`internal/server`)

All routes except `/health` require `Authorization: Bearer <api_token>` when
`api_token` is set (constant-time compare). The check runs before routing, so
unknown paths also give `401 {"error":"Unauthorized"}`. Unknown route with a
valid token: `404`, `text/plain`, body `Not Found`. Non-GET/HEAD on a known
route: `405`. JSON bodies are 2-space indented, no HTML escaping, no trailing
newline (`/health` is compact). Errors: `{"error":"<message>"}`.

**`GET /health`** (public) – `status` is always first; the status objects show
the last successful fetch/poll (UTC RFC3339, empty if none yet) and last error.

```json
{"status":"ok","calendar":{"last_success":"2026-11-10T21:00:00Z","last_error":""},"solar":{"last_success":"2026-11-10T21:04:12Z","last_error":""}}
```

**`GET /events`** – the `/events` contract above, with `max_events`, shift 0.
Calendar failure with no cache: `502 {"error":"<message>"}`.

**`GET /solar`** – the latest `Snapshot`; `503 {"error":"no solar data yet"}`
until the first successful poll.

```json
{
  "pv_w": 1000,
  "load_w": 500,
  "grid_w": -500,
  "battery_soc": 50,
  "battery_w": 0,
  "updated_at": "2026-11-10T06:39:12Z",
  "source": "fake",
  "stale": false
}
```

**`GET /kitchen[?shift=0|1]`** – one call for the battery-powered kitchen panel.
Keys in order `calendar` (identical structure to `/events`, uses
`kitchen_max_events`, `shift` moves "Today" to tomorrow for late evenings;
anything but 0/1 → `400`), `solar` (Snapshot, or `null` before the first poll)
and `server_time` (now in the configured timezone, RFC3339 with offset). A
calendar error gives `502` so the panel keeps its last screen.

```json
{
  "calendar": {
    "today": {
      "label": "Today",
      "date": "2026-11-10",
      "events": []
    },
    "tomorrow": {
      "label": "Tomorrow",
      "date": "2026-11-11",
      "events": []
    },
    "fetched_at": "2026-11-10T14:22:03.721Z"
  },
  "solar": {
    "pv_w": 1000,
    "load_w": 500,
    "grid_w": -500,
    "battery_soc": 50,
    "battery_w": 0,
    "updated_at": "2026-11-10T06:39:12Z",
    "source": "fake",
    "stale": false
  },
  "server_time": "2026-11-10T22:05:00+01:00"
}
```

## Config

JSON file via `--config`/`-c`; environment variables override file values.
Absent or `null` keys use the default.

| JSON key | Env var | Default |
|---|---|---|
| `icloud_username` | `ICLOUD_USERNAME` | required |
| `icloud_app_password` | `ICLOUD_APP_PASSWORD` | required |
| `calendar_name` | `CALENDAR_NAME` | `Family` |
| `port` | `PORT` | `3000` |
| `max_events` | `MAX_EVENTS` | `3` |
| `kitchen_max_events` | `KITCHEN_MAX_EVENTS` | `5` |
| `cache_ttl_seconds` | `CACHE_TTL_SECONDS` | `300` |
| `api_token` | `API_TOKEN` | empty (auth disabled) |
| `timezone` | `TIMEZONE` | `Europe/Zurich` |
| `solar_source` | `SOLAR_SOURCE` | `fake` (only supported value) |
| `solar_poll_seconds` | `SOLAR_POLL_SECONDS` | `60` |

## Calendar expansion

`internal/calendar.Expand` turns parsed iCalendar objects (`emersion/go-ical`)
into concrete occurrences overlapping a window `[from, to)`, all in the
configured timezone. Supported: `DTSTART`/`DTEND`/`DURATION` with `TZID`, UTC
(`Z`) and floating times (read in the configured zone), all-day (`VALUE=DATE`,
exclusive end), and the full `RRULE` feature set of `rrule-go` plus `RDATE` and
`EXDATE`. Per-instance changes (`RECURRENCE-ID` overrides with the same `UID`)
replace the generated instance. `STATUS:CANCELLED` events are skipped; missing
`SUMMARY` becomes `Untitled`. Not supported: `RANGE=THISANDFUTURE` overrides,
custom `VTIMEZONE` definitions (only IANA `TZID`s). Day grouping is done by
`calendar.Build` (see `/events` contract). A malformed event (unknown `TZID`,
unparsable `DTSTART`/`RRULE`) is skipped with a `slog` warning (UID + reason);
it never fails the whole calendar.

## Calendar data flow and caching

`calendar.Service` sits between the HTTP handlers and a `Fetcher`:

1. `Get(shift, max)` serves from the cache whenever one exists. The cache is
   *stale* if older than `cache_ttl_seconds` or fetched for a previous day
   (the window moves at local midnight; the old window still covers today..+2,
   so `Build` keeps working).
2. **Cold cache** (never fetched successfully): `Get` fetches synchronously,
   single-flight (concurrent callers wait on one fetch); a failure is returned
   (handlers map it to `502`).
3. **Warm but stale cache:** `Get` returns the cached data immediately and
   starts exactly ONE background refresh (own 30 s timeout context, not the
   request's). `/kitchen` therefore never waits on iCloud once warmed.
   The refresh fetches `[today 00:00, today+3d)` in the configured timezone
   and runs it through `Expand`.
4. **Stale on error / backoff:** if a background refresh fails the stale data
   keeps being served, `Status().LastError` is set, and no new attempt is made
   for 60 s. `Status().LastSuccess` is the last successful refresh.
5. `shift`/`max_events` are applied per request by `Build`, so all callers
   (`/events`, kitchen panel) share one cache.

`NewICloudFetcher` speaks CalDAV (`emersion/go-webdav`) to `caldav.icloud.com`
with basic auth (app-specific password): principal -> calendar-home-set ->
calendar lookup by trimmed, case-insensitive display name -> `calendar-query`
for `VEVENT` in the window. Discovery is cached after the first success. iCloud
answers the home-set with an absolute URL on a shard host
(`pNN-caldav.icloud.com`); go-webdav drops the host, so the raw PROPFIND
response is inspected and a new client is created for that host.

`home-apis --dump-events` (hidden debug flag) fetches once and prints the
`/events` JSON.

## Solar

`internal/solar` defines `Source` (`Name()`, `Read(ctx) (Snapshot, error)`),
a `Poller` and a deterministic fake source. Real GoodWe sources plug in later.

**Snapshot / sign conventions** (JSON keys in this order): `pv_w`, `load_w`,
`grid_w` (positive = import, negative = export), `battery_soc` (0-100),
`battery_w` (positive = charging, negative = discharging), `updated_at` (UTC
RFC3339, seconds precision), `source`, `stale`. Balance:
`pv_w - load_w - battery_w + grid_w == 0`.

**Fake source** (`solar_source: "fake"`, `Name()` = `fake`) is a pure function
of the clock in the configured timezone:
- PV = peak * sin(pi * (t - sunrise) / (sunset - sunrise)) between sunrise and
  sunset, else 0. A seasonal factor (cosine over day-of-year, 1 at June 21)
  moves sunrise 05:30 -> 08:10, sunset 21:20 -> 16:45 and peak 6.5 kW -> 2.5 kW.
- Load = 350 W base + 900 W Gaussian bumps (sigma 45 min) at 07:00 and 18:30.
- SoC: 20 % at 06:00, linear to 95 % at 15:00, linear down to 20 % at 06:00.
- Battery absorbs surplus up to 3 kW while SoC < 100; covers deficit up to
  3 kW while SoC > 20; the grid takes the remainder (so evenings import/discharge
  and early mornings at the 20 % floor import, exercising the panel warnings).
- PV, load and battery are rounded to 10 W; grid is derived.

**Poller:** `Run` polls immediately, then every `solar_poll_seconds`. Failures
keep the last good snapshot and set `Status()` last error. `Latest()` returns
`ok=false` until the first success and sets `stale` at read time when
`now - lastSuccess > 3 * every`; `updated_at` stays the source's timestamp.

## Deployment

Single static Go binary (`CGO_ENABLED=0`, linux/amd64, tzdata embedded), run by
systemd in a Debian LXC under `/opt/home-apis` (binary, `config.json`, `VERSION`).
The unit is `deploy/home-apis.service` (hardened: `NoNewPrivileges`,
`ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `ReadWritePaths=/opt/home-apis`).
`main` wires `calendar.Service` (iCloud fetcher), a `solar.Poller` goroutine and
`server.New` into an `http.Server` (`ReadHeaderTimeout` 5s) and shuts down
gracefully on SIGTERM/SIGINT. Startup logs the version, calendar, TTL, max
events, auth on/off and solar source. `--version` prints the build version.

Releases: pushing a tag `v*` runs `.github/workflows/release.yml` (vet, test,
static build with `-X main.version=<tag>`) and publishes `home-apis`,
`home-apis.sha256` and `install.sh` as GitHub release assets.

## Operations

- **Install / upgrade:** on the LXC as root,
  `curl -fsSL https://github.com/githendrik/home-apis/releases/latest/download/install.sh | sh`.
  It downloads the latest binary, writes `/opt/home-apis/VERSION`, creates
  `config.json` only if missing (never overwrites), installs the unit and
  enables + restarts the service. Edit `config.json` after the first install.
- **Logs:** `journalctl -u home-apis -f`.
- **Status:** `systemctl status home-apis`; `curl localhost:3000/health`.
- **Version:** `/opt/home-apis/home-apis --version` or `cat /opt/home-apis/VERSION`.
- **Rollback:** download the older binary
  (`https://github.com/githendrik/home-apis/releases/download/<tag>/home-apis`)
  to `/opt/home-apis/home-apis`, write the tag to `VERSION`, and
  `systemctl restart home-apis`.
- **Config validation:** `port`, `max_events`, `kitchen_max_events`,
  `cache_ttl_seconds` and `solar_poll_seconds` must be > 0, else startup fails.
