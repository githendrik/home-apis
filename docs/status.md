# Status

_Last updated: 2026-11-27._

## Deployment

| | |
|---|---|
| Version | **v0.3.0** (GitHub release; `cat /opt/home-apis/VERSION`) |
| Host | Proxmox host `lab`, unprivileged Debian 13 LXC **CT 110 `home-apis`**, DHCP, resolves as `home-apis` on the LAN |
| Service | systemd `home-apis`, port 3000, bearer auth on (token = `api_token` in the LXC config, shared with all panels) |
| Calendar | iCloud calendar "Familie" (credentials copied from CT 116) |
| Solar | `solar_source: sems` (GoodWe SEMS+ cloud, read-only visitor account, poll 300 s; history fetched every 5 min) |
| Consumers | Kitchen panel (`/kitchen`, incl. `history` for its chart; firmware v0.2.3). Desk panels still read the old service. |

The old Deno service (`icloud-calendar-api`, **CT 116 `calendar-api`**) still
runs unchanged and serves the desk panels.

## Parallel run (cut-over check)

CT 110 runs `/usr/local/bin/events-compare` every 30 min (`/etc/cron.d/events-compare`),
diffing old vs new `/events` into `/var/log/events-compare.log`.

Expected, accepted difference: recurring events after a DST change (e.g.
"Putzfrau" 13:30 new vs 12:30 old — the old service is wrong).

## Open items

1. **Cut-over (~2026-11-14):** review the compare log for unexpected diffs → `vzdump 116` → stop CT 116 → give CT 110 the `calendar-api` name/address (or repoint the desk panels to `home-apis`) → verify desk panels → remove the compare cron → archive the old repo after two weeks. Homelab logs: Obsidian `daily-log/2026-11-10-home-apis-deploy`, `2026-11-11-home-apis-calendar-fix`.
2. **Confirm SEMS signs on real data:** battery (`pBat` positive = discharge → our `battery_w` negative) once the battery moves; PV/export (`pGrid` positive = export) once the PV panels produce (they were still being mounted on 2026-11-26). If a sign is wrong, fix it in `sems.go` `snapshot()` and its test.
3. **Minor:** `/health` exposes upstream error text on the LAN; installer doesn't verify the checksum; service runs as root (hardened unit).

## Done recently

- **Real solar source (2026-11-26, v0.2.0):** `solar_source: "sems"` reads the GoodWe SEMS+ cloud with a read-only visitor account; the GoodWe stays on the isolated guest WiFi (see `docs/decisions.md` → Solar). Homelab log: Obsidian `daily-log/2026-11-26-home-apis-sems-solar`.
- **History for the kitchen chart (2026-11-26, v0.3.0):** `/kitchen` `history` (5-minute bins, see `docs/architecture.md`). Deployed the same day; the panel draws it since firmware v0.2.0 (v0.2.3 installed). Design: `eink-kitchen-panel/docs/superpowers/specs/2026-11-26-solar-history-chart-design.md`.

## History

- v0.1.0 (2026-11-10): Go port of the Deno calendar proxy + `/solar` (fake) + `/kitchen`; deployed next to CT 116.
- v0.1.1 (2026-11-11): fixed empty calendar — iCloud ignores go-webdav's partial calendar-data requests; own REPORT now.
- v0.2.0 (2026-11-26): `solar_source: "sems"` — GoodWe SEMS+ cloud source (read-only visitor account), `--dump-solar`. Deployed to CT 110 the same day.
- v0.3.0 (2026-11-26): `/kitchen` carries today's 5-minute production/consumption history (`history`), sent compact; `/health` shows the history status; `--dump-solar` prints a history summary. Deployed to CT 110 the same day.
