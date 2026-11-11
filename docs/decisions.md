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
| SEMS cloud is *not* the default | GoodWe retired the legacy SEMS portal (Sep 2026); SEMS+ API is signed and in flux, no personal developer tokens | Agent (research), user agreed |
| Preferred real source: read the inverter locally (UDP 8899 / Modbus TCP, as the Python `goodwe` library) | Stable, no account/token | Agent |
| **Open:** network placement of the GoodWe | Swisscom Internet-Box 5 Pro guest WiFi (10.x) is isolated from the main LAN (192.168.x) with no per-rule exception. Options: GoodWe on main WiFi + local read; GoodWe on guest WiFi + SEMS+ cloud; own VLAN gear | User to decide at installation |

## Units and conventions

- `grid_w` negative = export, positive = import; `battery_w` positive = charging; watts and SoC are integers.
- `updated_at` UTC RFC3339 seconds; `server_time` RFC3339 with Zurich offset.
