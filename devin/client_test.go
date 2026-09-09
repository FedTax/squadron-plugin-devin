package devin

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestListSessionsQuery(t *testing.T) {
	q := listSessionsQuery([]string{"DEV-8126", "rate-investigation"}, 5, "")

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
		if got := listSessionsQuery([]string{"DEV-1"}, limit, "").Get("limit"); got != "20" {
			t.Errorf("limit %d gave %q, want the default 20", limit, got)
		}
	}
}

func TestListSessionsQueryUserEmail(t *testing.T) {
	q := listSessionsQuery([]string{"DEV-1"}, 5, "svc@example.com")
	if got := q.Get("user_email"); got != "svc@example.com" {
		t.Errorf("user_email = %q, want the address passed", got)
	}
	if got := listSessionsQuery([]string{"DEV-1"}, 5, ""); got.Has("user_email") {
		t.Errorf("user_email = %q, want the parameter omitted entirely when empty", got.Get("user_email"))
	}
}

func TestAPIErrorForbidden(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   bool
	}{
		{http.StatusForbidden, true},
		{http.StatusUnauthorized, true},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false},
	} {
		err := &APIError{StatusCode: tc.status}
		if got := err.Forbidden(); got != tc.want {
			t.Errorf("status %d: Forbidden() = %v, want %v", tc.status, got, tc.want)
		}
		if got := isForbidden(error(err)); got != tc.want {
			t.Errorf("status %d: isForbidden() = %v, want %v", tc.status, got, tc.want)
		}
	}
	if isForbidden(errStub{}) {
		t.Error("isForbidden() on a non-API error = true, want false")
	}
}

type errStub struct{}

func (errStub) Error() string { return "not an api error" }

// TestSessionDetailV1ToStatus covers the mapping the v3 fallback depends on:
// v1 reports the lifecycle in status_enum, and a caller reading only the v3
// field names must still see a finished session as finished.
func TestSessionDetailV1ToStatus(t *testing.T) {
	detail := sessionDetailV1{
		SessionID:  "devin-abc123",
		Status:     "Devin is done",
		StatusEnum: "finished",
		Title:      "DEV-8909 investigation",
	}
	detail.PullRequest = &struct {
		URL string `json:"url"`
	}{URL: "https://github.com/FedTax/repo/pull/7"}

	status := detail.toStatus()
	if status.Status != "finished" || status.StatusDetail != "finished" {
		t.Errorf("status/detail = %q/%q, want the status_enum in both", status.Status, status.StatusDetail)
	}
	if want := "https://app.devin.ai/sessions/abc123"; status.URL != want {
		t.Errorf("url = %q, want %q", status.URL, want)
	}
	if len(status.PullRequests) != 1 || status.PullRequests[0].URL != "https://github.com/FedTax/repo/pull/7" {
		t.Errorf("pull requests = %v, want the v1 pull_request carried over", status.PullRequests)
	}
}

// TestSessionDetailV1FallsBackToStatusString guards the case where status_enum
// is absent: dropping to the free-text status beats reporting no status at all.
func TestSessionDetailV1FallsBackToStatusString(t *testing.T) {
	detail := sessionDetailV1{SessionID: "devin-x", Status: "working"}
	if got := detail.toStatus().Status; got != "working" {
		t.Errorf("status = %q, want the status string when status_enum is empty", got)
	}
}

// TestSessionDetailV1DecodesStructuredOutput checks the field the insights
// fallback exists for: a mission's routers read structured_output, so it has to
// survive the v1 shape unchanged.
func TestSessionDetailV1DecodesStructuredOutput(t *testing.T) {
	var detail sessionDetailV1
	body := `{"session_id":"devin-x","status_enum":"finished","structured_output":{"verdict":"BUG"}}`
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := `{"verdict":"BUG"}`; string(detail.StructuredOutput) != want {
		t.Errorf("structured output = %q, want %q", detail.StructuredOutput, want)
	}
}

// TestTimestampDecodesBothShapes covers the two wire formats a session time
// arrives in: v3 sends epoch seconds, v1 sends RFC 3339. A caller comparing the
// two would otherwise see one of them as 1970 and call a live session expired.
func TestTimestampDecodesBothShapes(t *testing.T) {
	var v3 SessionStatus
	if err := json.Unmarshal([]byte(`{"session_id":"devin-x","updated_at":1753700000}`), &v3); err != nil {
		t.Fatalf("unmarshal v3: %v", err)
	}
	if got := v3.UpdatedAt.Unix(); got != 1753700000 {
		t.Errorf("v3 updated_at = %d, want the epoch seconds it was sent as", got)
	}

	var v1 sessionDetailV1
	if err := json.Unmarshal([]byte(`{"session_id":"devin-x","updated_at":"2026-07-28T09:50:00Z"}`), &v1); err != nil {
		t.Fatalf("unmarshal v1: %v", err)
	}
	if got := v1.UpdatedAt.Format(time.RFC3339); got != "2026-07-28T09:50:00Z" {
		t.Errorf("v1 updated_at = %q, want the RFC 3339 instant it was sent as", got)
	}
}

// TestResumableTracksTheThirtyDayWindow is the check the routing depends on: a
// session past the window reads no differently in its status, so age is what
// tells a stage to open a new session rather than brief a dead one.
func TestResumableTracksTheThirtyDayWindow(t *testing.T) {
	recent := SessionStatus{UpdatedAt: Timestamp{time.Now().Add(-29 * 24 * time.Hour)}}
	if !recent.Resumable() {
		t.Error("a session idle 29 days is still within the window Devin resumes")
	}

	stale := SessionStatus{UpdatedAt: Timestamp{time.Now().Add(-31 * 24 * time.Hour)}}
	if stale.Resumable() {
		t.Error("a session idle 31 days is past the window; a message to it is refused")
	}

	// An unknown timestamp must not condemn a session: the refused send is the
	// evidence, and guessing the other way strands work that could continue.
	unknown := SessionStatus{}
	if !unknown.Resumable() {
		t.Error("a session with no timestamps reads as resumable")
	}
}

// TestLastActivityPrefersTheLaterTimestamp guards against reading a long-running
// session's age from its creation: v1 and v3 differ on which field they fill.
func TestLastActivityPrefersTheLaterTimestamp(t *testing.T) {
	created := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	status := SessionStatus{CreatedAt: Timestamp{created}, UpdatedAt: Timestamp{updated}}
	if got := status.LastActivity(); !got.Equal(updated) {
		t.Errorf("last activity = %s, want the update %s", got, updated)
	}

	onlyCreated := SessionStatus{CreatedAt: Timestamp{created}}
	if got := onlyCreated.LastActivity(); !got.Equal(created) {
		t.Errorf("last activity = %s, want the creation %s when there is no update", got, created)
	}
}
