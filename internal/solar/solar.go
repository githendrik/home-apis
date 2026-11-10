package solar

import (
	"context"
	"encoding/json"
	"time"
)

// Snapshot is one reading of the PV system.
// GridW: positive = import, negative = export. BatteryW: positive = charging.
type Snapshot struct {
	PVW        int
	LoadW      int
	GridW      int
	BatterySOC int
	BatteryW   int
	UpdatedAt  time.Time
	Source     string
	Stale      bool
}

type snapshotJSON struct {
	PVW        int    `json:"pv_w"`
	LoadW      int    `json:"load_w"`
	GridW      int    `json:"grid_w"`
	BatterySOC int    `json:"battery_soc"`
	BatteryW   int    `json:"battery_w"`
	UpdatedAt  string `json:"updated_at"`
	Source     string `json:"source"`
	Stale      bool   `json:"stale"`
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(snapshotJSON{
		PVW: s.PVW, LoadW: s.LoadW, GridW: s.GridW,
		BatterySOC: s.BatterySOC, BatteryW: s.BatteryW,
		UpdatedAt: s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Source:    s.Source, Stale: s.Stale,
	})
}

// Source produces solar snapshots.
type Source interface {
	Name() string
	Read(ctx context.Context) (Snapshot, error)
}
