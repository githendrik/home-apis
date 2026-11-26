# Decisions

"User" = the owner's explicit choice; "agent" = a call made during the build
that the owner can revisit.

## Service shape

| Decision | Why | By |
|---|---|---|
| A backend aggregates everything; panels only fetch one JSON | Integration churn (iCloud, GoodWe) never needs a firmware release; one HTTP call per wake saves panel battery | User + agent |
| Keep `/events` unchanged for the existing panels; new `/kitchen` endpoint for the kitchen panel | Other panels read `/events` | User |
| `/kitchen` = calendar + solar + `server_time` in one response, `?shift=1` for the 22:00 night screen | One request per wake; panel sets its clock from `server_time` | Agent |
| One service, one container for calendar + solar | Simpler to run | User |
| Solar polled in the background and cached; calendar served stale-while-revalidate with 60 s retry backoff | `/kitchen` must answer < 1 s, never wait on upstreams | Agent |

## Language and repo

| Decision | Why | By |
|---|---|---|
| Rewrite in Go (from Deno) | Deno development ends ~Oct 2027 after the Cloudflare acquisition | User |
| New repo `home-apis`, old repo archived after cut-over | Clean start | User |

## Calendar port

| Decision | Why | By |
|---|---|---|
| Same JSON contract, config keys and env vars as the Deno service | Drop-in replacement; old `config.json` reusable | Agent |
| All times as Europe/Zurich wall time without `Z` | Deno leaked UTC `…Z` for UTC events → wrong hour on panels | Agent |
| Real RRULE engine (INTERVAL, BYDAY, UNTIL, EXDATE, overrides) | Deno's approximation shifts recurring events after DST changes | Agent |
| Skip and log a malformed event instead of failing the whole calendar | One bad event must not blank every panel | Agent |
| Own CalDAV REPORT with plain `<c:calendar-data/>` | iCloud returns empty objects for go-webdav's partial-data requests (found in the parallel run) | Agent |
| Parallel run + automatic diff before cut-over; CT 116 stopped, not destroyed | Safe migration with rollback | Agent |

## Solar

| Decision | Why | By |
|---|---|---|
| Build with a realistic `fake` source first | Nothing real to work against yet; lets the panel be developed | User |
| ~~SEMS cloud is *not* the default~~ (superseded 2026-11-26, see below) | GoodWe retired the legacy SEMS portal (Sep 2026); SEMS+ API is signed and in flux, no personal developer tokens | Agent (research), user agreed |
| ~~Preferred real source: read the inverter locally (UDP 8899 / Modbus TCP, as the Python `goodwe` library)~~ (superseded by the network decision below) | Stable, no account/token | Agent |
| ~~**Open:** network placement of the GoodWe~~ → **decided 2026-11-26:** GoodWe stays on the isolated guest WiFi; `home-apis` reads the SEMS+ cloud | If the inverter is compromised (happens more often than one would think), the attacker must not land in the main network. Guest WiFi (10.x) can't reach the LAN (192.168.x), so a local read is ruled out | User |
| SEMS+ is workable after all: the "signature" is `sha256(ms@uid@token)` from login data, no secret key; maintained reference: `goodwe-sems-home-assistant` | Corrects the row above; checked live on 2026-11-26 | Agent |
| Read-only visitor account (plant shared view-only), never the owner login | A leaked `config.json` only exposes readings, can't control the inverter | User |
| Login at `eu-semsplus.goodwe.com`, not the global host | The account is registered in the EU region; the global login answered `100005` | Agent (found live) |
| Poll every 300 s | SEMS+ flow updates on 5-minute marks; the panel looks every 15 min; avoids SEMS rate limiting | Agent |
| 15 min pause after a rejected login, 5 min after rate limiting | Don't lock the account / don't hammer an unofficial API | Agent |
| One `flow` call per poll (station id discovered once) | It carries everything `/kitchen` needs | Agent |

## Kitchen history chart (2026-11-26)

| Decision | Why | By |
|---|---|---|
| `/kitchen` carries today's production/consumption history for the panel chart (design: `eink-kitchen-panel/docs/superpowers/specs/2026-11-26-solar-history-chart-design.md`) | The owner wants a feel for the day's production and when consumption exceeds it | User |
| 5-minute bins from 06:00, values in 100 W units, `null` for gaps | Fits the panel's 4000-byte RTC copy; finer than the 376-px chart needs | Agent |
| `/kitchen` sent compact instead of indented | Indented history arrays made a realistic body 6970 bytes; compact is ~3.4 KB. Only the panel reads it | Agent (found by the size test) |
| History fetched at most every 5 min, independent of `solar_poll_seconds` | One ~1440-point response per call; SEMS updates once a minute anyway | Agent |
| A rate limit / rejected login from either SEMS call pauses both | The limit is per account | Agent |
| History only for `shift=0` and only when dated today | The night screen has no chart; yesterday's curve must not show after midnight | Agent |

## Units and conventions

- `grid_w` negative = export, positive = import; `battery_w` positive = charging; watts and SoC are integers.
- `updated_at` UTC RFC3339 seconds; `server_time` RFC3339 with Zurich offset.
