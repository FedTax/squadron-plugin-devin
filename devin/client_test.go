package devin

import "testing"

func TestListSessionsQuery(t *testing.T) {
	q := listSessionsQuery([]string{"DEV-8126", "rate-investigation"}, 5)

	if got := q["tags"]; len(got) != 2 || got[0] != "DEV-8126" || got[1] != "rate-investigation" {
		t.Errorf("tags = %v, want each tag as its own parameter", got)
	}
	if got := q.Get("limit"); got != "5" {
		t.Errorf("limit = %q, want %q", got, "5")
	}
	if want := "limit=5&tags=DEV-8126&tags=rate-investigation"; q.Encode() != want {
		t.Errorf("encoded = %q, want %q", q.Encode(), want)
	}
}

func TestListSessionsQueryDefaultsLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if got := listSessionsQuery([]string{"DEV-1"}, limit).Get("limit"); got != "20" {
			t.Errorf("limit %d gave %q, want the default 20", limit, got)
		}
	}
}
