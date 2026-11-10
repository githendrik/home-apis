package calendar

import "testing"

func TestHomeSetHostFromXML(t *testing.T) {
	abs := `<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:propstat><d:prop>
<c:calendar-home-set><d:href>https://p123-caldav.icloud.com:443/1234/calendars/</d:href></c:calendar-home-set>
</d:prop></d:propstat></d:response></d:multistatus>`
	if got := homeSetHostFromXML([]byte(abs)); got != "p123-caldav.icloud.com:443" {
		t.Fatalf("got %q", got)
	}
	rel := `<multistatus xmlns="DAV:"><response><propstat><prop><calendar-home-set xmlns="urn:ietf:params:xml:ns:caldav"><href xmlns="DAV:">/1234/calendars/</href></calendar-home-set></prop></propstat></response></multistatus>`
	if got := homeSetHostFromXML([]byte(rel)); got != "" {
		t.Fatalf("got %q", got)
	}
}
