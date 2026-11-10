# Architecture

## Overview

`home-apis` is a small Go service that serves JSON for e-ink panels: iCloud
calendar events (port of the Deno `icloud-calendar-api`) and solar data.

## Components

- `cmd/home-apis` – entry point; loads config (tzdata embedded for minimal containers).
- `internal/config` – config loading (JSON file + env overrides).

## Endpoints

_None yet._

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
custom `VTIMEZONE` definitions (only IANA `TZID`s). Day grouping happens in a
separate step.

## Deployment

_To be documented._

## Operations

_To be documented._
