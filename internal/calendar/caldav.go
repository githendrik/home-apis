package calendar

import (
	"bytes"
	"context"
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
)

type icloudFetcher struct {
	username, password, calendarName string

	mu           sync.Mutex
	client       *caldav.Client // set after first successful discovery
	calendarPath string
}

// NewICloudFetcher returns a Fetcher for a named calendar on iCloud CalDAV.
// Discovery (principal, home set, calendar path) is cached after first success.
func NewICloudFetcher(username, appPassword, calendarName string) Fetcher {
	return &icloudFetcher{username: username, password: appPassword, calendarName: calendarName}
}

func (f *icloudFetcher) Fetch(ctx context.Context, from, to time.Time) ([]*ical.Calendar, error) {
	f.mu.Lock()
	client, path := f.client, f.calendarPath
	f.mu.Unlock()

	if client == nil {
		var err error
		client, path, err = f.discover(ctx)
		if err != nil {
			return nil, err
		}
		f.mu.Lock()
		f.client, f.calendarPath = client, path
		f.mu.Unlock()
	}

	objs, err := client.QueryCalendar(ctx, path, &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:     "VCALENDAR",
			AllProps: true,
			Comps:    []caldav.CalendarCompRequest{{Name: "VEVENT", AllProps: true}},
		},
		CompFilter: caldav.CompFilter{
			Name:  "VCALENDAR",
			Comps: []caldav.CompFilter{{Name: "VEVENT", Start: from, End: to}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("caldav query: %w", err)
	}
	cals := make([]*ical.Calendar, 0, len(objs))
	for _, o := range objs {
		if o.Data != nil {
			cals = append(cals, o.Data)
		}
	}
	return cals, nil
}

func (f *icloudFetcher) discover(ctx context.Context) (*caldav.Client, string, error) {
	rec := &hrefRecorder{inner: webdav.HTTPClientWithBasicAuth(http.DefaultClient, f.username, f.password)}
	c, err := caldav.NewClient(rec, icloudEndpoint)
	if err != nil {
		return nil, "", err
	}
	principal, err := c.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("find principal: %w", err)
	}
	home, err := c.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return nil, "", fmt.Errorf("find calendar home: %w", err)
	}

	// go-webdav keeps only the path of the home-set href, but iCloud returns an
	// absolute URL on another host (e.g. p123-caldav.icloud.com). Use the host
	// recorded from the raw response for all further requests.
	if host := rec.homeSetHost(); host != "" && strings.TrimSuffix(host, ":443") != icloudHost {
		slog.Info("calendar home on different host", "host", host)
		c, err = caldav.NewClient(rec, "https://"+host+"/")
		if err != nil {
			return nil, "", err
		}
	}

	cals, err := c.FindCalendars(ctx, home)
	if err != nil {
		return nil, "", fmt.Errorf("list calendars: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(f.calendarName))
	for _, cal := range cals {
		if strings.ToLower(strings.TrimSpace(cal.Name)) == want {
			slog.Info("calendar discovered", "name", cal.Name, "path", cal.Path)
			return c, cal.Path, nil
		}
	}
	names := make([]string, 0, len(cals))
	for _, cal := range cals {
		names = append(names, cal.Name)
	}
	return nil, "", fmt.Errorf("calendar %q not found (available: %v)", f.calendarName, names)
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
