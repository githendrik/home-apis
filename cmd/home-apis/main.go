package main

import (
	"fmt"
	"os"
	_ "time/tzdata"

	"github.com/githendrik/home-apis/internal/config"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	fmt.Printf("home-apis: config loaded (port %d, tz %s)\n", cfg.Port, cfg.Timezone)
}
