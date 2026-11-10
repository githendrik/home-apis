// Package config loads service configuration from a JSON file and environment variables.
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ICloudUsername    string
	ICloudAppPassword string
	CalendarName      string
	Port              int
	MaxEvents         int
	KitchenMaxEvents  int
	CacheTTLSeconds   int
	SolarPollSeconds  int
	APIToken          string // "" = auth disabled
	Timezone          *time.Location
	SolarSource       string
}

// fileConfig uses pointers so that absent and null both mean "use default".
type fileConfig struct {
	ICloudUsername    *string `json:"icloud_username"`
	ICloudAppPassword *string `json:"icloud_app_password"`
	CalendarName      *string `json:"calendar_name"`
	Port              *int    `json:"port"`
	MaxEvents         *int    `json:"max_events"`
	CacheTTLSeconds   *int    `json:"cache_ttl_seconds"`
	APIToken          *string `json:"api_token"`
	Timezone          *string `json:"timezone"`
	KitchenMaxEvents  *int    `json:"kitchen_max_events"`
	SolarSource       *string `json:"solar_source"`
	SolarPollSeconds  *int    `json:"solar_poll_seconds"`
}

// Load reads config. args excludes the program name. Env vars override the file.
func Load(args []string, getenv func(string) string) (Config, error) {
	fs := flag.NewFlagSet("home-apis", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var path string
	fs.StringVar(&path, "config", "", "path to JSON config file")
	fs.StringVar(&path, "c", "", "path to JSON config file (shorthand)")
	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parsing flags: %w", err)
	}

	var f fileConfig
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("reading config file %s: %w", path, err)
		}
		if err := json.Unmarshal(data, &f); err != nil {
			return Config{}, fmt.Errorf("parsing config file %s: %w", path, err)
		}
	}

	str := func(env string, file *string, def string) string {
		if v := getenv(env); v != "" {
			return v
		}
		if file != nil && *file != "" {
			return *file
		}
		return def
	}
	var firstErr error
	num := func(env string, file *int, def int) int {
		if v := getenv(env); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s must be an integer, got %q", env, v)
			}
			return n
		}
		if file != nil {
			return *file
		}
		return def
	}

	c := Config{
		ICloudUsername:    str("ICLOUD_USERNAME", f.ICloudUsername, ""),
		ICloudAppPassword: str("ICLOUD_APP_PASSWORD", f.ICloudAppPassword, ""),
		CalendarName:      str("CALENDAR_NAME", f.CalendarName, "Family"),
		Port:              num("PORT", f.Port, 3000),
		MaxEvents:         num("MAX_EVENTS", f.MaxEvents, 3),
		KitchenMaxEvents:  num("KITCHEN_MAX_EVENTS", f.KitchenMaxEvents, 5),
		CacheTTLSeconds:   num("CACHE_TTL_SECONDS", f.CacheTTLSeconds, 300),
		SolarPollSeconds:  num("SOLAR_POLL_SECONDS", f.SolarPollSeconds, 60),
		APIToken:          str("API_TOKEN", f.APIToken, ""),
		SolarSource:       str("SOLAR_SOURCE", f.SolarSource, "fake"),
	}
	if firstErr != nil {
		return Config{}, firstErr
	}
	if c.ICloudUsername == "" {
		return Config{}, fmt.Errorf("icloud_username is required (ICLOUD_USERNAME env var or config file)")
	}
	if c.ICloudAppPassword == "" {
		return Config{}, fmt.Errorf("icloud_app_password is required (ICLOUD_APP_PASSWORD env var or config file)")
	}
	switch c.SolarSource {
	case "fake":
	default:
		return Config{}, fmt.Errorf("unknown solar_source %q (supported: fake)", c.SolarSource)
	}
	tz := str("TIMEZONE", f.Timezone, "Europe/Zurich")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	c.Timezone = loc
	return c, nil
}
