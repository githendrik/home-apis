package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/githendrik/home-apis/internal/calendar"
	"github.com/githendrik/home-apis/internal/config"
	"github.com/githendrik/home-apis/internal/solar"
)

type CalendarProvider interface {
	Get(ctx context.Context, shift, max int) (calendar.Response, error)
	Status() calendar.Status
}

type SolarProvider interface {
	Latest() (solar.Snapshot, bool)
	Status() (time.Time, string)
}

type statusJSON struct {
	LastSuccess string `json:"last_success"`
	LastError   string `json:"last_error"`
}

type healthJSON struct {
	Status   string     `json:"status"`
	Calendar statusJSON `json:"calendar"`
	Solar    statusJSON `json:"solar"`
}

type kitchenJSON struct {
	Calendar   json.RawMessage `json:"calendar"`
	Solar      *solar.Snapshot `json:"solar"`
	ServerTime string          `json:"server_time"`
}

type srv struct {
	cfg config.Config
	cal CalendarProvider
	sol SolarProvider
	now func() time.Time
}

func New(cfg config.Config, cal CalendarProvider, sol SolarProvider, now func() time.Time) http.Handler {
	s := &srv{cfg: cfg, cal: cal, sol: sol, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.events)
	mux.HandleFunc("/solar", s.solar)
	mux.HandleFunc("/kitchen", s.kitchen)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Not Found"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			s.health(w, r)
			return
		}
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *srv) authorized(r *http.Request) bool {
	if s.cfg.APIToken == "" {
		return true
	}
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(h[len("Bearer "):]), []byte(s.cfg.APIToken)) == 1
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	b, _ := json.Marshal(map[string]string{"error": msg})
	writeJSON(w, status, b)
}

// encode renders v with 2-space indent, no HTML escaping, no trailing newline.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func onlyGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return false
	}
	return true
}

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *srv) health(w http.ResponseWriter, _ *http.Request) {
	cs := s.cal.Status()
	sl, se := s.sol.Status()
	b, _ := json.Marshal(healthJSON{
		Status:   "ok",
		Calendar: statusJSON{ts(cs.LastSuccess), cs.LastError},
		Solar:    statusJSON{ts(sl), se},
	})
	writeJSON(w, http.StatusOK, b)
}

func (s *srv) events(w http.ResponseWriter, r *http.Request) {
	if !onlyGet(w, r) {
		return
	}
	resp, err := s.cal.Get(r.Context(), 0, s.cfg.MaxEvents)
	if err != nil {
		slog.Error("fetch events", "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var buf bytes.Buffer
	if err := calendar.Encode(&buf, resp); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buf.Bytes())
}

func (s *srv) solar(w http.ResponseWriter, r *http.Request) {
	if !onlyGet(w, r) {
		return
	}
	snap, ok := s.sol.Latest()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "no solar data yet")
		return
	}
	b, err := encode(snap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *srv) kitchen(w http.ResponseWriter, r *http.Request) {
	if !onlyGet(w, r) {
		return
	}
	shift := 0
	switch r.URL.Query().Get("shift") {
	case "", "0":
	case "1":
		shift = 1
	default:
		writeError(w, http.StatusBadRequest, "shift must be 0 or 1")
		return
	}
	resp, err := s.cal.Get(r.Context(), shift, s.cfg.KitchenMaxEvents)
	if err != nil {
		slog.Error("fetch kitchen events", "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var cb bytes.Buffer
	if err := calendar.Encode(&cb, resp); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := kitchenJSON{
		Calendar:   cb.Bytes(),
		ServerTime: s.now().In(s.cfg.Timezone).Format(time.RFC3339),
	}
	if snap, ok := s.sol.Latest(); ok {
		out.Solar = &snap
	}
	b, err := encode(out)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}
