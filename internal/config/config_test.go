package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func noEnv(string) string { return "" }

func TestLoadDefaults(t *testing.T) {
	p := writeFile(t, `{"icloud_username":"u","icloud_app_password":"p"}`)
	c, err := Load([]string{"--config", p}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.CalendarName != "Family" || c.Port != 3000 || c.MaxEvents != 3 ||
		c.CacheTTLSeconds != 300 || c.KitchenMaxEvents != 5 ||
		c.SolarSource != "fake" || c.SolarPollSeconds != 60 ||
		c.Timezone.String() != "Europe/Zurich" || c.APIToken != "" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestNonPositiveValuesRejected(t *testing.T) {
	for _, key := range []string{"port", "max_events", "kitchen_max_events", "cache_ttl_seconds", "solar_poll_seconds"} {
		for _, v := range []string{"0", "-1"} {
			p := writeFile(t, `{"icloud_username":"u","icloud_app_password":"p","`+key+`":`+v+`}`)
			_, err := Load([]string{"--config", p}, noEnv)
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%s: expected error mentioning key, got %v", key, v, err)
			}
		}
	}
}

func TestEnvOverridesFile(t *testing.T) {
	p := writeFile(t, `{"icloud_username":"u","icloud_app_password":"p","port":3000,"api_token":"file"}`)
	env := map[string]string{"PORT": "4000", "API_TOKEN": "env"}
	c, err := Load([]string{"--config", p}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 4000 || c.APIToken != "env" {
		t.Fatalf("env did not override: %+v", c)
	}
}

func TestMissingCredentials(t *testing.T) {
	p := writeFile(t, `{"icloud_app_password":"p"}`)
	_, err := Load([]string{"--config", p}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "icloud_username") {
		t.Fatalf("expected icloud_username error, got %v", err)
	}
}

func TestDenoConfigCompatible(t *testing.T) {
	p := writeFile(t, `{
  "icloud_username": "your@apple.id",
  "icloud_app_password": "xxxx-xxxx-xxxx-xxxx",
  "calendar_name": "Family",
  "port": 3000,
  "max_events": 3,
  "cache_ttl_seconds": 300,
  "api_token": null
}`)
	c, err := Load([]string{"--config", p}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.APIToken != "" {
		t.Fatalf("APIToken = %q", c.APIToken)
	}
}

func TestShortFlag(t *testing.T) {
	p := writeFile(t, `{"icloud_username":"u","icloud_app_password":"p","port":1234}`)
	c, err := Load([]string{"-c", p}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 1234 {
		t.Fatalf("Port = %d", c.Port)
	}
}

func TestUnknownSolarSource(t *testing.T) {
	p := writeFile(t, `{"icloud_username":"u","icloud_app_password":"p","solar_source":"nope"}`)
	if _, err := Load([]string{"-c", p}, noEnv); err == nil {
		t.Fatal("expected error")
	}
}
