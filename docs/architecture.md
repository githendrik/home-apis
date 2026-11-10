# Architecture

## Overview

`home-apis` is a small Go service that serves JSON for e-ink panels: iCloud
calendar events (port of the Deno `icloud-calendar-api`) and solar data.

## Components

- `cmd/home-apis` – entry point; loads config (tzdata embedded for minimal containers).
- `internal/config` – config loading (JSON file + env overrides).

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
`calendar.Build` (see `/events` contract).

## Deployment

_To be documented._

## Operations

_To be documented._
