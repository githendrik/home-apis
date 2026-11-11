package calendar

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

const (
	icloudHost     = "caldav.icloud.com"
	icloudEndpoint = "https://" + icloudHost + "/"
	httpTimeout    = 30 * time.Second
)

type icloudFetcher struct {
	username, password, calendarName string

	mu           sync.Mutex
	httpc        webdav.HTTPClient // authenticated client, set after first successful discovery
	base         string            // scheme://host of the calendar home (iCloud partition host)
	calendarPath string
}

// NewICloudFetcher returns a Fetcher for a named calendar on iCloud CalDAV.
// Discovery (principal, home set, calendar path) is cached after first success.
func NewICloudFetcher(username, appPassword, calendarName string) Fetcher {
	return &icloudFetcher{username: username, password: appPassword, calendarName: calendarName}
}

func (f *icloudFetcher) Fetch(ctx context.Context, from, to time.Time) ([]*ical.Calendar, error) {
	f.mu.Lock()
	httpc, base, path := f.httpc, f.base, f.calendarPath
	f.mu.Unlock()

	if httpc == nil {
		var err error
		httpc, base, path, err = f.discover(ctx)
		if err != nil {
			return nil, err
		}
		f.mu.Lock()
		f.httpc, f.base, f.calendarPath = httpc, base, path
		f.mu.Unlock()
	}

	cals, err := reportCalendar(ctx, httpc, base+path, from, to)
	if err != nil {
		// Drop cached discovery so the next attempt rediscovers (rotated
		// password, moved calendar).
		f.mu.Lock()
		f.httpc, f.base, f.calendarPath = nil, "", ""
		f.mu.Unlock()
		return nil, fmt.Errorf("caldav query: %w", err)
	}
	slog.Debug("caldav query", "from", from, "to", to, "calendars", len(cals))
	return cals, nil
}

// reportCalendar runs a CalDAV calendar-query asking for plain
// <calendar-data/>. go-webdav always sends a partial-data <comp> request,
// which iCloud answers with empty VCALENDAR shells.
func reportCalendar(ctx context.Context, httpc webdav.HTTPClient, calURL string, from, to time.Time) ([]*ical.Calendar, error) {
	const layout = "20060102T150405Z"
	body := `<?xml version="1.0" encoding="utf-8"?>` +
		`<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<d:prop><d:getetag/><c:calendar-data/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT">` +
		`<c:time-range start="` + from.UTC().Format(layout) + `" end="` + to.UTC().Format(layout) + `"/>` +
		`</c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	req, err := http.NewRequestWithContext(ctx, "REPORT", calURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("REPORT %s: %s", calURL, resp.Status)
	}
	var ms struct {
		Responses []struct {
			Propstats []struct {
				CalendarData string `xml:"prop>calendar-data"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("decode multistatus: %w", err)
	}
	var cals []*ical.Calendar
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if strings.TrimSpace(ps.CalendarData) == "" {
				continue
			}
			cal, err := ical.NewDecoder(strings.NewReader(ps.CalendarData)).Decode()
			if err != nil {
				slog.Warn("skipping undecodable calendar object", "reason", err)
				continue
			}
			cals = append(cals, cal)
		}
	}
	return cals, nil
}

// discover finds the calendar and returns an authenticated HTTP client, the
// base URL (scheme://host) of the calendar home, and the calendar path.
func (f *icloudFetcher) discover(ctx context.Context) (webdav.HTTPClient, string, string, error) {
	rec := &hrefRecorder{inner: webdav.HTTPClientWithBasicAuth(&http.Client{Timeout: httpTimeout}, f.username, f.password)}
	base := "https://" + icloudHost
	c, err := caldav.NewClient(rec, icloudEndpoint)
	if err != nil {
		return nil, "", "", err
	}
	principal, err := c.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("find principal: %w", err)
	}
	home, err := c.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return nil, "", "", fmt.Errorf("find calendar home: %w", err)
	}

	// go-webdav keeps only the path of the home-set href, but iCloud returns an
	// absolute URL on another host (e.g. p123-caldav.icloud.com). Use the host
	// recorded from the raw response for all further requests.
	if host := rec.homeSetHost(); host != "" && strings.TrimSuffix(host, ":443") != icloudHost {
		slog.Info("calendar home on different host", "host", host)
		base = "https://" + host
		c, err = caldav.NewClient(rec, base+"/")
		if err != nil {
			return nil, "", "", err
		}
	}

	cals, err := c.FindCalendars(ctx, home)
	if err != nil {
		return nil, "", "", fmt.Errorf("list calendars: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(f.calendarName))
	for _, cal := range cals {
		if strings.ToLower(strings.TrimSpace(cal.Name)) == want {
			slog.Info("calendar discovered", "name", cal.Name, "path", cal.Path)
			return rec, base, cal.Path, nil
		}
	}
	names := make([]string, 0, len(cals))
	for _, cal := range cals {
		names = append(names, cal.Name)
	}
	return nil, "", "", fmt.Errorf("calendar %q not found (available: %v)", f.calendarName, names)
}

var homeSetRe = regexp.MustCompile(`(?s)calendar-home-set[^>]*>\s*<(?:[\w-]+:)?href[^>]*>\s*([^<\s]+)\s*<`)

// hrefRecorder wraps an HTTP client and remembers the host of an absolute
// calendar-home-set href seen in any PROPFIND response.
type hrefRecorder struct {
	inner webdav.HTTPClient
	mu    sync.Mutex
	host  string
}

func (r *hrefRecorder) Do(req *http.Request) (*http.Response, error) {
	resp, err := r.inner.Do(req)
	if err != nil || req.Method != "PROPFIND" || resp.Body == nil {
		return resp, err
	}
	body, rerr := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if rerr != nil {
		return resp, nil
	}
	if h := homeSetHostFromXML(body); h != "" {
		r.mu.Lock()
		r.host = h
		r.mu.Unlock()
	}
	return resp, nil
}

func (r *hrefRecorder) homeSetHost() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.host
}

func homeSetHostFromXML(body []byte) string {
	m := homeSetRe.FindSubmatch(body)
	if m == nil {
		return ""
	}
	u, err := url.Parse(string(m[1]))
	if err != nil || u.Host == "" || u.Scheme != "https" {
		return ""
	}
	return u.Host
}
