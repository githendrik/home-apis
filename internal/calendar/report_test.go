package calendar

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// iCloud answers partial calendar-data requests (<comp ...>) with empty
// VCALENDAR shells, so the fetcher must ask for plain <calendar-data/>.
const icloudReportResponse = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<multistatus xmlns="DAV:">
    <response>
        <href>/1/calendars/abc/E1E9F51C.ics</href>
        <propstat>
            <prop>
                <calendar-data xmlns="urn:ietf:params:xml:ns:caldav"><![CDATA[BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//iCloud//EN
BEGIN:VEVENT
UID:E1E9F51C-FB5E-4C33-BB85-A3307EA89148
DTSTAMP:20261101T120000Z
DTSTART;TZID=Europe/Zurich:20261112T070000
DTEND;TZID=Europe/Zurich:20261112T180000
SUMMARY:KiTa geschlossen
END:VEVENT
END:VCALENDAR
]]></calendar-data>
                <getetag>"abc"</getetag>
            </prop>
            <status>HTTP/1.1 200 OK</status>
        </propstat>
    </response>
</multistatus>`

func TestFetchUsesPlainCalendarDataAndParsesEvents(t *testing.T) {
	var gotMethod, gotDepth, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotDepth, gotBody, gotPath = r.Method, r.Header.Get("Depth"), string(b), r.URL.Path
		if strings.Contains(gotBody, "<c:comp ") || strings.Contains(gotBody, "<comp ") {
			// Mimic iCloud: partial calendar-data requests yield empty calendars.
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, strings.Replace(icloudReportResponse,
				icloudReportResponse[strings.Index(icloudReportResponse, "VERSION:2.0"):strings.Index(icloudReportResponse, "END:VCALENDAR")], "", 1))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, icloudReportResponse)
	}))
	defer srv.Close()

	f := &icloudFetcher{httpc: srv.Client(), base: srv.URL, calendarPath: "/1/calendars/abc/"}
	loc, _ := time.LoadLocation("Europe/Zurich")
	from := time.Date(2026, 11, 11, 0, 0, 0, 0, loc)
	cals, err := f.Fetch(context.Background(), from, from.AddDate(0, 0, 3))
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "REPORT" || gotDepth != "1" || gotPath != "/1/calendars/abc/" {
		t.Fatalf("request = %s depth=%q path=%q", gotMethod, gotDepth, gotPath)
	}
	if !strings.Contains(gotBody, "<c:calendar-data/>") || !strings.Contains(gotBody, `start="20261110T230000Z"`) {
		t.Fatalf("unexpected body: %s", gotBody)
	}
	if len(cals) != 1 || len(cals[0].Events()) != 1 {
		t.Fatalf("got %d calendars", len(cals))
	}
	occ, err := Expand(cals, from, from.AddDate(0, 0, 3), loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 1 || occ[0].Summary != "KiTa geschlossen" || occ[0].Start.Hour() != 7 {
		t.Fatalf("occurrences = %+v", occ)
	}
}
