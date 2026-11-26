package solar

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SEMS+ Web (the browser app at eu-semsplus.goodwe.com). Undocumented API;
// request shapes follow the web app and the goodwe-sems-home-assistant
// integration.
const (
	DefaultSemsLoginURL = "https://eu-semsplus.goodwe.com/web/sems/sems-user/api/v1/auth/cross-login"
	semsFallbackAPI     = "https://eu-gateway.semsportal.com/web/sems"
	semsUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	semsLoginRejectedWait = 15 * time.Minute
	semsRateLimitWait     = 5 * time.Minute
)

var (
	errSemsTokenExpired = errors.New("sems: token expired")
	errSemsRateLimited  = errors.New("sems: rate limited")
)

// SemsOptions configures NewSems. LoginURL, HTTP and Now have defaults.
type SemsOptions struct {
	Username string
	Password string
	LoginURL string
	HTTP     *http.Client
	Now      func() time.Time
	Loc      *time.Location // time zone of the flow's refreshTime
}

type sems struct {
	opts SemsOptions

	mu         sync.Mutex
	tokenJSON  string // login data, sent back verbatim in the "token" header
	uid, token string
	api        string
	station    string
	waitUntil  time.Time
	waitReason string
}

// NewSems returns a Source that reads the live power flow of the first
// station visible to a (read-only) SEMS+ account.
func NewSems(o SemsOptions) Source {
	if o.LoginURL == "" {
		o.LoginURL = DefaultSemsLoginURL
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Loc == nil {
		o.Loc = time.UTC
	}
	return &sems{opts: o}
}

func (s *sems) Name() string { return "sems" }

func (s *sems) Read(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.opts.Now()
	if err := s.waitErr(now); err != nil {
		return Snapshot{}, err
	}

	var flow map[string]any
	err := s.withSession(ctx, func() error {
		path := "/sems-plant/api/stations/flow?stationId=" + url.QueryEscape(s.station)
		if err := s.call(ctx, http.MethodGet, s.api+path, nil, s.authHeaders(), &flow); err != nil {
			return fmt.Errorf("sems flow: %w", err)
		}
		return nil
	})
	s.noteWait(now, err)
	if err != nil {
		return Snapshot{}, err
	}
	return s.snapshot(flow, now)
}

// History reads today's per-minute production and consumption curve and bins it.
func (s *sems) History(ctx context.Context) (History, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.opts.Now()
	if err := s.waitErr(now); err != nil {
		return History{}, err
	}
	day := now.In(s.opts.Loc).Format("2006-01-02")
	var resp struct {
		DataList []struct {
			Item      string `json:"item"`
			PowerData []struct {
				Tp    string `json:"tp"`
				Power any    `json:"power"`
			} `json:"powerData"`
		} `json:"dataList"`
	}
	err := s.withSession(ctx, func() error {
		body := map[string]any{
			"stationId": s.station, "items": []string{"pSystem", "pConsum"},
			"timeScale": 1, "timeZone": 1,
			"startTime": day + " 00:00:00", "endTime": day + " 23:59:59",
			"needDispatchLimit": true,
		}
		if err := s.call(ctx, http.MethodPost, s.api+"/sems-plant/api/v1/hems/power/statisticsAndPreV2", body, s.authHeaders(), &resp); err != nil {
			return fmt.Errorf("sems history: %w", err)
		}
		return nil
	})
	s.noteWait(now, err)
	if err != nil {
		return History{}, err
	}

	series := map[string][]MinutePoint{}
	for _, d := range resp.DataList {
		for _, p := range d.PowerData {
			kw, ok := semsNumber(map[string]any{"v": p.Power}, "v")
			if !ok {
				continue
			}
			t, err := time.ParseInLocation("2006-01-02 15:04:05", p.Tp, s.opts.Loc)
			if err != nil {
				continue
			}
			series[d.Item] = append(series[d.Item], MinutePoint{At: t, W: kw * 1000})
		}
	}
	h := History{Date: day, UpdatedAt: now}
	h.PV, h.PVWh = BuildHistory(series["pSystem"], now, s.opts.Loc, now)
	h.Load, h.LoadWh = BuildHistory(series["pConsum"], now, s.opts.Loc, now)
	return h, nil
}

func (s *sems) waitErr(now time.Time) error {
	if now.Before(s.waitUntil) {
		return fmt.Errorf("sems: %s, next attempt after %s",
			s.waitReason, s.waitUntil.UTC().Format(time.RFC3339))
	}
	return nil
}

// noteWait pauses all SEMS calls after a rejected login or a rate limit.
func (s *sems) noteWait(now time.Time, err error) {
	var rejected *semsLoginRejected
	switch {
	case errors.As(err, &rejected):
		s.wait(now, semsLoginRejectedWait, "login rejected")
	case errors.Is(err, errSemsRateLimited):
		s.wait(now, semsRateLimitWait, "rate limited")
	}
}

func (s *sems) wait(now time.Time, d time.Duration, reason string) {
	s.waitUntil, s.waitReason = now.Add(d), reason
}

// withSession logs in and discovers the station when needed, runs fn, and
// logs in again once if the token has expired.
func (s *sems) withSession(ctx context.Context, fn func() error) error {
	for attempt := 0; ; attempt++ {
		err := s.ensureSession(ctx)
		if err == nil {
			err = fn()
		}
		if errors.Is(err, errSemsTokenExpired) && attempt == 0 {
			s.tokenJSON = ""
			continue
		}
		return err
	}
}

func (s *sems) ensureSession(ctx context.Context) error {
	if s.tokenJSON == "" {
		if err := s.login(ctx); err != nil {
			return err
		}
	}
	if s.station == "" {
		return s.discoverStation(ctx)
	}
	return nil
}

type semsLoginRejected struct{ code, desc string }

func (e *semsLoginRejected) Error() string {
	return fmt.Sprintf("sems: login rejected (code %s: %s)", e.code, e.desc)
}

func (s *sems) login(ctx context.Context) error {
	sum := md5.Sum([]byte(s.opts.Password))
	body := map[string]any{
		"account":   s.opts.Username,
		"pwd":       base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(sum[:]))),
		"agreement": 1,
		"isChinese": false,
		"isLocal":   false,
	}
	origin := s.opts.LoginURL
	if u, err := url.Parse(s.opts.LoginURL); err == nil {
		origin = u.Scheme + "://" + u.Host
	}
	headers := map[string]string{
		"Accept":      "application/json, text/plain, */*",
		"Origin":      origin,
		"Referer":     origin + "/",
		"Token":       `{"uid":"","timestamp":0,"token":"","client":"semsPlusWeb","version":"","language":"en"}`,
		"X-Signature": semsSignature(s.opts.Now(), "", ""),
	}
	var data json.RawMessage
	err := s.call(ctx, http.MethodPost, s.opts.LoginURL, body, headers, &data)
	var codeErr *semsCodeError
	if errors.As(err, &codeErr) {
		return &semsLoginRejected{code: codeErr.code, desc: codeErr.desc}
	}
	if err != nil {
		return fmt.Errorf("sems login: %w", err)
	}
	var tok struct {
		UID   string `json:"uid"`
		Token string `json:"token"`
		API   string `json:"api"`
	}
	if err := json.Unmarshal(data, &tok); err != nil || tok.Token == "" {
		return errors.New("sems login: response without token")
	}
	s.tokenJSON, s.uid, s.token = string(data), tok.UID, tok.Token
	s.api = strings.TrimRight(tok.API, "/")
	if s.api == "" {
		s.api = semsFallbackAPI
	}
	slog.Info("sems logged in", "api", s.api)
	return nil
}

func (s *sems) discoverStation(ctx context.Context) error {
	var page struct {
		DataList []struct {
			ID string `json:"id"`
		} `json:"dataList"`
	}
	body := map[string]any{"current": 1, "size": 100}
	if err := s.call(ctx, http.MethodPost, s.api+"/sems-plant/api/portal/stations/page", body, s.authHeaders(), &page); err != nil {
		return fmt.Errorf("sems station list: %w", err)
	}
	if len(page.DataList) == 0 || page.DataList[0].ID == "" {
		return errors.New("sems: account sees no station")
	}
	if len(page.DataList) > 1 {
		slog.Warn("sems account sees several stations, using the first", "count", len(page.DataList))
	}
	s.station = page.DataList[0].ID
	slog.Info("sems station", "id", s.station)
	return nil
}

func (s *sems) authHeaders() map[string]string {
	return map[string]string{
		"Accept":      "application/json",
		"token":       s.tokenJSON,
		"X-Signature": semsSignature(s.opts.Now(), s.uid, s.token),
	}
}

// semsSignature: base64(hex(sha256("ms@uid@token")) + "@" + ms).
func semsSignature(now time.Time, uid, token string) string {
	ms := strconv.FormatInt(now.UnixMilli(), 10)
	sum := sha256.Sum256([]byte(ms + "@" + uid + "@" + token))
	return base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(sum[:]) + "@" + ms))
}

type semsCodeError struct{ code, desc string }

func (e *semsCodeError) Error() string { return fmt.Sprintf("code %s: %s", e.code, e.desc) }

// call performs one request and decodes the envelope's data into out.
func (s *sems) call(ctx context.Context, method, rawURL string, body any, headers map[string]string, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", semsUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.opts.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errSemsTokenExpired
	case resp.StatusCode == http.StatusTooManyRequests:
		return errSemsRateLimited
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var env struct {
		Code        json.RawMessage `json:"code"`
		Description string          `json:"description"`
		Msg         string          `json:"msg"`
		Data        json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	code := strings.Trim(string(env.Code), `"`)
	switch code {
	case "0", "00000":
	case "C0602", "C0607", "100002":
		return errSemsTokenExpired
	case "GY0429":
		return errSemsRateLimited
	default:
		desc := env.Description
		if desc == "" {
			desc = env.Msg
		}
		return &semsCodeError{code: code, desc: desc}
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return errors.New("response without data")
	}
	return json.Unmarshal(env.Data, out)
}

func (s *sems) snapshot(flow map[string]any, now time.Time) (Snapshot, error) {
	pv, okPV := semsNumber(flow, "pSystem")
	if !okPV {
		pv, okPV = semsNumber(flow, "pAc")
	}
	load, okLoad := semsNumber(flow, "pConsum")
	grid, okGrid := semsNumber(flow, "pGrid")
	if !okPV && !okLoad && !okGrid {
		return Snapshot{}, errors.New("sems: flow without power readings")
	}
	bat, _ := semsNumber(flow, "pBat")
	soc, _ := semsNumber(flow, "soc")

	updated := now.UTC()
	if rt, ok := flow["refreshTime"].(string); ok {
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", rt, s.opts.Loc); err == nil {
			updated = t.UTC()
		}
	}
	watts := func(kw float64) int { return int(math.Round(kw * 1000)) }
	return Snapshot{
		PVW:        watts(pv),
		LoadW:      watts(load),
		GridW:      -watts(grid), // SEMS+: positive pGrid = export
		BatteryW:   -watts(bat),  // SEMS+: positive pBat = discharge
		BatterySOC: int(math.Round(soc)),
		UpdatedAt:  updated,
		Source:     "sems",
	}, nil
}

// semsNumber reads a number that SEMS+ may send as a JSON number or string.
func semsNumber(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}
