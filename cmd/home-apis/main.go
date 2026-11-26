package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/githendrik/home-apis/internal/calendar"
	"github.com/githendrik/home-apis/internal/config"
	"github.com/githendrik/home-apis/internal/server"
	"github.com/githendrik/home-apis/internal/solar"
)

var version = "dev"

func main() {
	args, dump := stripFlag(os.Args[1:], "--dump-events")
	args, dumpSolarFlag := stripFlag(args, "--dump-solar")
	args, showVersion := stripFlag(args, "--version")
	if showVersion {
		fmt.Println(version)
		return
	}
	cfg, err := config.Load(args, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if dumpSolarFlag {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		if err := dumpSolar(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "dump-solar:", err)
			os.Exit(1)
		}
		return
	}
	if dump {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		if err := dumpEvents(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "dump-events:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	ttl := time.Duration(cfg.CacheTTLSeconds) * time.Second
	cal := calendar.NewService(
		calendar.NewICloudFetcher(cfg.ICloudUsername, cfg.ICloudAppPassword, cfg.CalendarName),
		ttl, cfg.Timezone, time.Now)
	poller := solar.NewPoller(newSolarSource(cfg),
		time.Duration(cfg.SolarPollSeconds)*time.Second, time.Now)
	go poller.Run(ctx)

	httpSrv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           server.New(cfg, cal, poller, time.Now),
		ReadHeaderTimeout: 5 * time.Second,
	}

	auth := "off"
	if cfg.APIToken != "" {
		auth = "on"
	}
	log.Printf("home-apis %s listening on %s", version, httpSrv.Addr)
	log.Printf("calendar=%q cache_ttl=%s max_events=%d kitchen_max_events=%d auth=%s tz=%s",
		cfg.CalendarName, ttl, cfg.MaxEvents, cfg.KitchenMaxEvents, auth, cfg.Timezone)
	log.Printf("solar source=%s poll=%ds", cfg.SolarSource, cfg.SolarPollSeconds)

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Println("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutCtx)
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

func newSolarSource(cfg config.Config) solar.Source {
	if cfg.SolarSource == "sems" {
		return solar.NewSems(solar.SemsOptions{
			Username: cfg.SemsUsername,
			Password: cfg.SemsPassword,
			Loc:      cfg.Timezone,
		})
	}
	return solar.NewFake(time.Now, cfg.Timezone)
}

// dumpSolar reads the configured solar source once and prints the snapshot
// (debug aid).
func dumpSolar(cfg config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	snap, err := newSolarSource(cfg).Read(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}
