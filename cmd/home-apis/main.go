package main

import (
	"context"
	"fmt"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/githendrik/home-apis/internal/calendar"
	"github.com/githendrik/home-apis/internal/config"
)

func main() {
	args, dump := stripFlag(os.Args[1:], "--dump-events")
	cfg, err := config.Load(args, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if dump {
		if err := dumpEvents(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "dump-events:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Printf("home-apis: config loaded (port %d, tz %s)\n", cfg.Port, cfg.Timezone)
}

func stripFlag(args []string, name string) ([]string, bool) {
	var out []string
	found := false
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}

// dumpEvents fetches the calendar once and prints the /events JSON (debug aid).
func dumpEvents(cfg config.Config) error {
	if cfg.ICloudUsername == "" || cfg.ICloudAppPassword == "" {
		return fmt.Errorf("icloud_username and icloud_app_password must be configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	svc := calendar.NewService(
		calendar.NewICloudFetcher(cfg.ICloudUsername, cfg.ICloudAppPassword, cfg.CalendarName),
		time.Duration(cfg.CacheTTLSeconds)*time.Second, cfg.Timezone, time.Now)
	resp, err := svc.Get(ctx, 0, 3)
	if err != nil {
		return err
	}
	return calendar.Encode(os.Stdout, resp)
}
