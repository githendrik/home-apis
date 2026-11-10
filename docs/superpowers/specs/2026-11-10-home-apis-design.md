# home-apis — design

Date: 2026-11-10 · Status: draft for review · Repo: `githendrik/home-apis` ·
Companion: `eink-kitchen-panel/docs/superpowers/specs/2026-11-10-kitchen-panel-firmware-design.md`

## Intent

A single small Go service on the homelab that feeds all e-ink panels. It replaces
the Deno-based `icloud-calendar-api` (Deno's runtime development ends ~Oct 2027
after the Cloudflare acquisition) and adds GoodWe solar data for the new kitchen
panel. Panels stay dumb: they make one HTTP call and render. All integration
churn (iCloud quirks, GoodWe/SEMS API changes) is absorbed here, so it never
requires a firmware release.

## Goals

- Port `GET /events` from `~/Repos/github/githendrik/icloud-calendar-api` with a
  **byte-compatible response** (same JSON shape, field names, ordering semantics,
  recurrence expansion, today/tomorrow split, timezone handling), so existing
  panels (eink-desk-panel) keep working unchanged.
- New `GET /solar`: current PV production, house consumption, grid import/export,
  home battery SoC and charge/discharge power, plus data timestamp.
- New `GET /kitchen`: calendar + solar combined in one response, sized for a
  battery-powered client that wakes briefly.
- Run both in one new LXC container on `lab`.

## Non-goals

- No UI, no history/graphs, no database.
- No writing to the inverter (read-only).
- Not a general Home-Assistant replacement.

## Anti-goals

- Breaking the existing `/events` consumers during migration.
- A slow or failing GoodWe source degrading `/events` or `/kitchen` latency.
- Panels needing to know anything about SEMS, tokens, or the inverter network.

## Decisions (by the user)

- Language: **Go**. New repo `home-apis` (not an in-place rewrite); old repo
  archived after cut-over.
- Calendar proxy and solar live in **one service, one container**.
- `/events` stays as is for other panels; kitchen panel uses a separate endpoint.
- GoodWe will likely be on a **separate WiFi** from the panels. SEMS cloud is
  acceptable if tokens can be long-lived; local UDP is the fallback.

## Design

### Endpoints

| Route | Consumer | Notes |
|---|---|---|
| `GET /events` | existing panels | Exact port. Same optional `Authorization: Bearer` behaviour. |
| `GET /solar` | debugging, future panels | Served from in-memory cache, never blocks on GoodWe. |
| `GET /kitchen` | kitchen panel | `{calendar: <same as /events body>, solar: <same as /solar body or null>, server_time}` |
| `GET /healthz` | monitoring | Reports age/status of calendar and solar sources. |

`/kitchen` accepts `?shift=1` (or equivalent) to return calendar for
tomorrow/day-after in the today/tomorrow slots — used by the panel's 22:00
"night" screen. (Builder may pick the exact parameter name.)

Proposed `/solar` body (builder may refine names; units are fixed):

```json
{
  "pv_w": 2400,
  "load_w": 900,
  "grid_w": -1100,          // negative = exporting, positive = importing
  "battery_soc": 78,        // percent
  "battery_w": 400,         // positive = charging, negative = discharging
  "updated_at": "2026-11-10T06:39:12Z",
  "source": "sems" | "local" | "fake",
  "stale": false
}
```

`server_time` lets the panel set its clock/timezone sanity without NTP if needed.

### Solar source (pluggable)

Interface with three implementations, selected by config:

1. **fake** — realistic synthetic day curve. Default until the GoodWe is installed;
   lets firmware be developed now.
2. **sems** — SEMS cloud via the unofficial CrossLogin + monitor-detail API
   (the OpenAPI is organization-accounts-only). Stores username/password in
   config, re-logs-in automatically when the token expires.
3. **local** — direct UDP to the inverter (port 8899, protocol as in the Python
   `goodwe` library), only if the LXC can route to the GoodWe network.

A background poller (default every 60 s) fills the cache. If the source fails,
the last value is served with `stale: true` and its original `updated_at`.

### Deployment

- New unprivileged Debian LXC on `lab`, static Go binary under
  `/opt/home-apis`, systemd unit with the same hardening as the current
  `icloud-calendar-api.service`. Config in a JSON file (iCloud CalDAV creds,
  SEMS creds, bearer token, timezone, poll interval).
- Follow the homelab skill: plan, daily log, rollback.
- **Cut-over:** run new service alongside CT 116, diff `/events` output from both
  over a few days (including recurring and all-day events), then move the
  `calendar-api` hostname/IP to the new container. CT 116 is stopped (not
  destroyed) as rollback.

## Constraints

- Panels speak plain HTTP on the LAN with an optional bearer token (existing behaviour).
- `/kitchen` must respond in well under 1 s (panel battery budget); everything is cached.
- Timezone: Europe/Zurich.

## Documentation (user requirement)

Keep `docs/architecture.md` in this repo current as part of every build step
(not as an afterthought): components and data flow (iCloud / GoodWe → poller →
cache → endpoints → panels), endpoint contracts with example JSON, config keys,
deployment (LXC, systemd, hostname), and operations (cut-over, rollback,
switching solar source). Link to the kitchen panel's architecture doc. A reader
months later should understand the system without reading code.

## Left to the builder

- Go module layout, HTTP router, CalDAV/iCal library choice.
- Calendar caching interval (current Deno behaviour is the reference).
- Exact field names beyond the units above, CI and release mechanics.

## Open items

- Confirm SEMS token lifetime and rate limits once the account exists.
- Decide whether the LXC can reach the GoodWe WiFi (enables `local` source).
