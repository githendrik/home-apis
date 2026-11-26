package solar

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	semsTestUser     = "visitor@example.com"
	semsTestPassword = "s3cret-pass"
	semsTestUID      = "uid-123"
	semsTestToken    = "tok-abc"
	semsTestStation  = "station-1"
)

// fakeSems imitates the SEMS+ Web endpoints the source uses.
type fakeSems struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	logins        int
	stationCalls  int
	flowCalls     int
	loginCode     string
	flowCodes     []string // consumed one per flow call; "" or empty = success
	stations      []string
	flow          map[string]any
	badSignatures int
	statsCalls    int
	statsCodes    []string // consumed one per statistics call; "" = success
	statsBody     map[string]any
}

func newFakeSems(t *testing.T) *fakeSems {
	f := &fakeSems{
		t:         t,
		loginCode: "00000",
		stations:  []string{semsTestStation},
		flow: map[string]any{
			"id": semsTestStation, "name": "Plant", "status": "1",
			"pSystem": 1.234, "pAc": 1.2, "pBat": 0.5, "pGrid": -0.808,
			"soc": 10, "pConsum": 0.808, "refreshTime": "2026-11-26T10:55:00",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /web/sems/sems-user/api/v1/auth/cross-login", f.login)
	mux.HandleFunc("POST /gw/sems-plant/api/portal/stations/page", f.stationsPage)
	mux.HandleFunc("GET /gw/sems-plant/api/stations/flow", f.stationFlow)
	mux.HandleFunc("POST /gw/sems-plant/api/v1/hems/power/statisticsAndPreV2", f.statistics)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSems) loginURL() string {
	return f.srv.URL + "/web/sems/sems-user/api/v1/auth/cross-login"
}

func writeSems(w http.ResponseWriter, code string, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"code": code, "description": "desc " + code, "data": data})
}

// checkSignature verifies X-Signature = base64(hex(sha256(ms@uid@token)) + "@" + ms).
func (f *fakeSems) checkSignature(r *http.Request, uid, token string) bool {
	raw, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Signature"))
	if err != nil {
		return false
	}
	digest, ms, ok := strings.Cut(string(raw), "@")
	if !ok {
		return false
	}
	if _, err := strconv.ParseInt(ms, 10, 64); err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(ms + "@" + uid + "@" + token))
	return digest == hex.EncodeToString(sum[:])
}

func (f *fakeSems) login(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logins++
	var body struct {
		Account string `json:"account"`
		Pwd     string `json:"pwd"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if !f.checkSignature(r, "", "") {
		f.badSignatures++
	}
	sum := md5.Sum([]byte(semsTestPassword))
	wantPwd := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(sum[:])))
	if body.Account != semsTestUser || body.Pwd != wantPwd {
		writeSems(w, "100005", nil)
		return
	}
	if f.loginCode != "00000" {
		writeSems(w, f.loginCode, nil)
		return
	}
	writeSems(w, "00000", map[string]any{
		"uid": semsTestUID, "token": semsTestToken, "timestamp": 1,
		"client": "semsPlusWeb", "version": "", "language": "en",
		"api": f.srv.URL + "/gw",
	})
}

// authorised checks the token header and signature of a data request.
func (f *fakeSems) authorised(r *http.Request) bool {
	var tok struct {
		UID   string `json:"uid"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(r.Header.Get("token")), &tok); err != nil {
		return false
	}
	if !f.checkSignature(r, tok.UID, tok.Token) {
		f.badSignatures++
		return false
	}
	return tok.UID == semsTestUID && tok.Token == semsTestToken
}

func (f *fakeSems) stationsPage(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stationCalls++
	if !f.authorised(r) {
		writeSems(w, "C0602", nil)
		return
	}
	var list []map[string]any
	for _, id := range f.stations {
		list = append(list, map[string]any{"id": id, "stationName": "Plant"})
	}
	writeSems(w, "00000", map[string]any{"dataList": list})
}

func (f *fakeSems) stationFlow(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flowCalls++
	if !f.authorised(r) {
		writeSems(w, "C0602", nil)
		return
	}
	if r.URL.Query().Get("stationId") != semsTestStation {
		writeSems(w, "B0001", nil)
		return
	}
	if len(f.flowCodes) > 0 {
		code := f.flowCodes[0]
		f.flowCodes = f.flowCodes[1:]
		if code != "" {
			writeSems(w, code, nil)
			return
		}
	}
	writeSems(w, "00000", f.flow)
}

// statistics serves today's curve: 06:00-06:14 at pSystem 1.2 kW and
// pConsum 0.5 kW (date taken from the request), plus the end-of-day sentinel.
func (f *fakeSems) statistics(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statsCalls++
	if !f.authorised(r) {
		writeSems(w, "C0602", nil)
		return
	}
	json.NewDecoder(r.Body).Decode(&f.statsBody)
	if len(f.statsCodes) > 0 {
		code := f.statsCodes[0]
		f.statsCodes = f.statsCodes[1:]
		if code != "" {
			writeSems(w, code, nil)
			return
		}
	}
	day := strings.Fields(fmt.Sprint(f.statsBody["startTime"]))[0]
	series := func(item string, kw float64) map[string]any {
		var pts []map[string]any
		for m := 0; m < 15; m++ {
			pts = append(pts, map[string]any{"tp": fmt.Sprintf("%s 06:%02d:00", day, m), "power": kw})
		}
		pts = append(pts, map[string]any{"tp": day + " 23:59:00", "power": nil, "isCE": true})
		return map[string]any{"item": item, "unit": "kW", "powerData": pts}
	}
	writeSems(w, "00000", map[string]any{"dataList": []any{
		series("pSystem", 1.2), series("pConsum", 0.5),
	}})
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestSems(t *testing.T, f *fakeSems, password string) (Source, *testClock) {
	t.Helper()
	clock := &testClock{t: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)}
	return NewSems(SemsOptions{
		Username: semsTestUser,
		Password: password,
		LoginURL: f.loginURL(),
		HTTP:     f.srv.Client(),
		Now:      clock.now,
		Loc:      zurich(t),
	}), clock
}

func TestSemsReadConvertsFlowToSnapshot(t *testing.T) {
	f := newFakeSems(t)
	src, _ := newTestSems(t, f, semsTestPassword)

	got, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		PVW:        1234,
		LoadW:      808,
		GridW:      808,  // SEMS+ negative pGrid = import; ours positive
		BatteryW:   -500, // SEMS+ positive pBat = discharge; ours negative
		BatterySOC: 10,
		UpdatedAt:  time.Date(2026, 11, 26, 9, 55, 0, 0, time.UTC), // 10:55 Zurich
		Source:     "sems",
	}
	if got != want {
		t.Fatalf("snapshot\n got %+v\nwant %+v", got, want)
	}
	if src.Name() != "sems" {
		t.Fatalf("name %q", src.Name())
	}
}

func TestSemsSignsEveryRequest(t *testing.T) {
	f := newFakeSems(t)
	src, _ := newTestSems(t, f, semsTestPassword)
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.badSignatures != 0 {
		t.Fatalf("%d requests with a bad X-Signature", f.badSignatures)
	}
}

func TestSemsLogsInOnceAcrossReads(t *testing.T) {
	f := newFakeSems(t)
	src, clock := newTestSems(t, f, semsTestPassword)
	for range 3 {
		if _, err := src.Read(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock.add(5 * time.Minute)
	}
	if f.logins != 1 || f.stationCalls != 1 || f.flowCalls != 3 {
		t.Fatalf("logins=%d stationCalls=%d flowCalls=%d, want 1/1/3", f.logins, f.stationCalls, f.flowCalls)
	}
}

func TestSemsLogsInAgainWhenTokenExpires(t *testing.T) {
	for _, code := range []string{"C0602", "C0607", "100002"} {
		t.Run(code, func(t *testing.T) {
			f := newFakeSems(t)
			src, _ := newTestSems(t, f, semsTestPassword)
			if _, err := src.Read(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.flowCodes = []string{code}
			got, err := src.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.LoadW != 808 {
				t.Fatalf("unexpected snapshot %+v", got)
			}
			if f.logins != 2 {
				t.Fatalf("logins=%d, want 2", f.logins)
			}
		})
	}
}

func TestSemsGivesUpAfterOneRelogin(t *testing.T) {
	f := newFakeSems(t)
	src, _ := newTestSems(t, f, semsTestPassword)
	f.flowCodes = []string{"C0602", "C0602", "C0602"}
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if f.logins != 2 {
		t.Fatalf("logins=%d, want 2", f.logins)
	}
}

func TestSemsRejectedLoginWaitsFifteenMinutes(t *testing.T) {
	f := newFakeSems(t)
	src, clock := newTestSems(t, f, "wrong-password")

	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	clock.add(14 * time.Minute)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected error while waiting")
	}
	if f.logins != 1 {
		t.Fatalf("logins=%d during wait, want 1", f.logins)
	}
	clock.add(2 * time.Minute)
	src.Read(context.Background())
	if f.logins != 2 {
		t.Fatalf("logins=%d after wait, want 2", f.logins)
	}
}

func TestSemsRateLimitWaitsFiveMinutes(t *testing.T) {
	f := newFakeSems(t)
	src, clock := newTestSems(t, f, semsTestPassword)
	f.flowCodes = []string{"GY0429"}

	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	clock.add(4 * time.Minute)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected error while waiting")
	}
	if f.flowCalls != 1 {
		t.Fatalf("flowCalls=%d during wait, want 1", f.flowCalls)
	}
	clock.add(2 * time.Minute)
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatalf("after wait: %v", err)
	}
}

func TestSemsErrorsDoNotContainSecrets(t *testing.T) {
	f := newFakeSems(t)
	src, _ := newTestSems(t, f, semsTestPassword)
	src.Read(context.Background())
	f.flowCodes = []string{"C0602", "C0602", "C0602"}
	_, err := src.Read(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{semsTestPassword, semsTestToken, semsTestUser} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks %q", err, secret)
		}
	}
}

func TestSemsNoStationIsAnError(t *testing.T) {
	f := newFakeSems(t)
	f.stations = nil
	src, _ := newTestSems(t, f, semsTestPassword)
	if _, err := src.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "station") {
		t.Fatalf("expected no-station error, got %v", err)
	}
}

func TestSemsFallsBackToPAcWithoutPSystem(t *testing.T) {
	f := newFakeSems(t)
	delete(f.flow, "pSystem")
	src, _ := newTestSems(t, f, semsTestPassword)
	got, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.PVW != 1200 {
		t.Fatalf("PVW=%d, want 1200 from pAc", got.PVW)
	}
}

func TestSemsMissingRefreshTimeUsesNow(t *testing.T) {
	f := newFakeSems(t)
	delete(f.flow, "refreshTime")
	src, clock := newTestSems(t, f, semsTestPassword)
	got, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(clock.now()) {
		t.Fatalf("UpdatedAt=%v, want %v", got.UpdatedAt, clock.now())
	}
}

func TestSemsFlowWithoutReadingsIsAnError(t *testing.T) {
	f := newFakeSems(t)
	f.flow = map[string]any{"id": semsTestStation, "status": "-1"}
	src, _ := newTestSems(t, f, semsTestPassword)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected error for a flow without readings")
	}
}

func TestSemsAcceptsNumericStrings(t *testing.T) {
	f := newFakeSems(t)
	f.flow["pConsum"] = "0.5"
	src, _ := newTestSems(t, f, semsTestPassword)
	got, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.LoadW != 500 {
		t.Fatalf("LoadW=%d, want 500", got.LoadW)
	}
}

func semsHistory(t *testing.T, src Source) (History, error) {
	t.Helper()
	hs, ok := src.(HistorySource)
	if !ok {
		t.Fatal("sems source does not implement HistorySource")
	}
	return hs.History(context.Background())
}

// 06:15 Zurich on 2026-11-26.
func setMorning(c *testClock) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = time.Date(2026, 11, 26, 5, 15, 0, 0, time.UTC)
}

func TestSemsHistoryBinsBothSeries(t *testing.T) {
	f := newFakeSems(t)
	src, clock := newTestSems(t, f, semsTestPassword)
	setMorning(clock)

	h, err := semsHistory(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if h.Date != "2026-11-26" || len(h.PV) != 3 || len(h.Load) != 3 {
		t.Fatalf("history %+v", h)
	}
	for i := range 3 {
		if h.PV[i] != (Bin{1200, true}) || h.Load[i] != (Bin{500, true}) {
			t.Fatalf("bin %d: pv %+v load %+v", i, h.PV[i], h.Load[i])
		}
	}
	if h.PVWh != 300 || h.LoadWh != 125 { // 15 min at 1.2 kW / 0.5 kW
		t.Fatalf("wh pv=%d load=%d", h.PVWh, h.LoadWh)
	}
	if !h.UpdatedAt.Equal(clock.now()) {
		t.Fatalf("UpdatedAt %v", h.UpdatedAt)
	}
	b := f.statsBody
	if b["stationId"] != semsTestStation || b["startTime"] != "2026-11-26 00:00:00" || b["endTime"] != "2026-11-26 23:59:59" {
		t.Fatalf("request body %+v", b)
	}
	items := fmt.Sprint(b["items"])
	if !strings.Contains(items, "pSystem") || !strings.Contains(items, "pConsum") {
		t.Fatalf("items %v", items)
	}
	if f.badSignatures != 0 {
		t.Fatalf("%d bad signatures", f.badSignatures)
	}
}

func TestSemsHistoryRateLimitPausesFlow(t *testing.T) {
	f := newFakeSems(t)
	src, clock := newTestSems(t, f, semsTestPassword)
	f.statsCodes = []string{"GY0429"}
	if _, err := semsHistory(t, src); err == nil {
		t.Fatal("expected error")
	}
	clock.add(4 * time.Minute)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("expected Read to wait after a rate-limited history call")
	}
	if f.flowCalls != 0 {
		t.Fatalf("flowCalls=%d during wait, want 0", f.flowCalls)
	}
}

func TestSemsHistoryReloginOnExpiry(t *testing.T) {
	f := newFakeSems(t)
	src, clock := newTestSems(t, f, semsTestPassword)
	setMorning(clock)
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.statsCodes = []string{"C0602"}
	h, err := semsHistory(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.PV) != 3 || f.logins != 2 {
		t.Fatalf("bins=%d logins=%d, want 3/2", len(h.PV), f.logins)
	}
}
