# Status

_Last updated: 2026-11-26._

## Deployment

| | |
|---|---|
| Version | **v0.1.1** (GitHub release; `cat /opt/home-apis/VERSION`) |
| Host | Proxmox host `lab`, unprivileged Debian 13 LXC **CT 110 `home-apis`**, DHCP, resolves as `home-apis` on the LAN |
| Service | systemd `home-apis`, port 3000, bearer auth on (token = `api_token` in the LXC config, shared with all panels) |
| Calendar | iCloud calendar "Familie" (credentials copied from CT 116) |
| Solar | `solar_source: fake` |
| Consumers | Kitchen panel (`/kitchen`). Desk panels still read the old service. |

The old Deno service (`icloud-calendar-api`, **CT 116 `calendar-api`**) still
runs unchanged and serves the desk panels.

## Parallel run (cut-over check)

CT 110 runs `/usr/local/bin/events-compare` every 30 min (`/etc/cron.d/events-compare`),
diffing old vs new `/events` into `/var/log/events-compare.log`.

Expected, accepted difference: recurring events after a DST change (e.g.
"Putzfrau" 13:30 new vs 12:30 old — the old service is wrong).

## Open items

1. **Cut-over (~2026-11-14):** review the compare log for unexpected diffs → `vzdump 116` → stop CT 116 → give CT 110 the `calendar-api` name/address (or repoint the desk panels to `home-apis`) → verify desk panels → remove the compare cron → archive the old repo after two weeks. Homelab logs: Obsidian `daily-log/2026-11-10-home-apis-deploy`, `2026-11-11-home-apis-calendar-fix`.
2. **Real solar source: built, not yet deployed** (branch `feat/sems-solar-source`). `solar_source: "sems"` reads the GoodWe SEMS+ cloud with a read-only visitor account (GoodWe stays on the guest WiFi, see `docs/decisions.md`). Verified live from the Mac on 2026-11-26 (`--dump-solar`). To go live: release, then in `/opt/home-apis/config.json` set `solar_source: "sems"`, `solar_poll_seconds: 300`, `sems_username`/`sems_password`; run `--dump-solar`, restart. Still to confirm on real data: battery sign (`pBat` positive = discharge) once the battery moves, PV/export sign once the panels produce.
3. **History chart for the kitchen panel (next):** SEMS+ `statisticsAndPreV2` gives per-minute PV/load/grid/battery/SoC for the day (see `docs/architecture.md`). Open: `/kitchen` contract, behaviour shortly after midnight, panel layout.
4. **Minor:** `/health` exposes upstream error text on the LAN; installer doesn't verify the checksum; service runs as root (hardened unit).

## History

- v0.1.0 (2026-11-10): Go port of the Deno calendar proxy + `/solar` (fake) + `/kitchen`; deployed next to CT 116.
- v0.1.1 (2026-11-11): fixed empty calendar — iCloud ignores go-webdav's partial calendar-data requests; own REPORT now.
