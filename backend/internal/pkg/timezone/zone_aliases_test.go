package timezone

import (
	"context"
	"testing"
	"time"
)

func TestUserTimezoneCanonicalSQLNamesPreserveCalendar(t *testing.T) {
	for _, pair := range [][2]string{{"America/Indianapolis", "America/Indiana/Indianapolis"}, {"Asia/Calcutta", "Asia/Kolkata"}, {"Asia/Katmandu", "Asia/Kathmandu"}, {"US/Eastern", "America/New_York"}, {"UTC", "UTC"}, {"Europe/Oslo", "Europe/Oslo"}} {
		t.Run(pair[0], func(t *testing.T) {
			ctx := WithUserDay(context.Background(), pair[0])
			got := UserTimezone(ctx)
			if got != pair[1] {
				t.Fatalf("SQL zone %s, want %s", got, pair[1])
			}
			canonical, err := time.LoadLocation(got)
			if err != nil {
				t.Fatal(err)
			}
			original, err := time.LoadLocation(pair[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, stamp := range []string{"2026-01-15T12:00:00Z", "2026-07-15T12:00:00Z", "2026-03-08T07:30:00Z", "2026-11-01T06:30:00Z"} {
				at, _ := time.Parse(time.RFC3339, stamp)
				if at.In(canonical).Format(time.RFC3339) != at.In(original).Format(time.RFC3339) {
					t.Fatalf("calendar changed for %s", stamp)
				}
			}
		})
	}
}
