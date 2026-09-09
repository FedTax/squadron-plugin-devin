// Package devin provides an HTTP client for the Devin AI v3 API.
package devin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	baseURL = "https://api.devin.ai/v3"

	// v1BaseURL serves the session list endpoint, which is the only documented
	// way to filter sessions by tag.
	v1BaseURL = "https://api.devin.ai/v1"

	// defaultListLimit caps a tag search that does not ask for a size.
	defaultListLimit = 20

	// Polling configuration
	defaultPollInterval = 15 * time.Second
	defaultPollTimeout  = 60 * time.Minute

	// maxPollErrors is the number of consecutive transient errors tolerated
	// during polling before giving up.
	maxPollErrors = 5
)

// APIError carries the HTTP status of a failed Devin API call, so a caller can
// distinguish "this key may not read that" from "the call went wrong". The
// organization-scoped v3 reads are gated on ViewOrgSessions, which a service
// user with the default Member role does not hold; the per-session v1 reads are
// scoped to what the key itself can see. Telling the two apart is what lets the
// v3 reads fall back to v1 rather than failing the stage.
type APIError struct {
	StatusCode int
	Endpoint   string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("devin API error (status %d) from %s: %s", e.StatusCode, e.Endpoint, e.Body)
}

// Forbidden reports whether the call was refused for lack of permission, as
// opposed to failing for any other reason.
func (e *APIError) Forbidden() bool {
	return e.StatusCode == http.StatusForbidden || e.StatusCode == http.StatusUnauthorized
}

// isForbidden reports whether err is a permission refusal from the API.
func isForbidden(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Forbidden()
}

// Client communicates with the Devin AI v3 API.
type Client struct {
	apiKey     string
	orgID      string
	httpClient *http.Client
}

// NewClient creates a new Devin API client.
func NewClient(apiKey, orgID string) *Client {
	return &Client{
		apiKey: apiKey,
		orgID:  orgID,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// orgURL returns the base URL for organization-scoped endpoints.
func (c *Client) orgURL() string {
	return baseURL + "/organizations/" + c.orgID
}

// CreateSessionRequest is the payload for creating a new Devin session.
type CreateSessionRequest struct {
	Prompt string   `json:"prompt"`
	Repos  []string `json:"repos,omitempty"`
	Title  string   `json:"title,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

// CreateSessionResponse is returned when a session is created.
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Status    string `json:"status"`
}

// SessionStatus represents the current state of a Devin session.
type SessionStatus struct {
	SessionID    string        `json:"session_id"`
	Status       string        `json:"status"`
	StatusDetail string        `json:"status_detail"`
	Title        string        `json:"title"`
	URL          string        `json:"url"`
	PullRequests []PullRequest `json:"pull_requests,omitempty"`
	IsArchived   bool          `json:"is_archived"`
	CreatedAt    Timestamp     `json:"created_at"`
	UpdatedAt    Timestamp     `json:"updated_at"`
}

// resumeWindow is how long Devin will continue a session for. Past it the
// session is readable but dead: a send is refused and the web app offers a new
// session instead. Nothing in the status says so — a month-old session still
// reads "suspended (inactivity)", the same as one suspended an hour ago — so
// the age is the only signal a caller has.
//
// See https://docs.devin.ai/admin/common-issues#session-expiration
const resumeWindow = 30 * 24 * time.Hour

// Timestamp is a session time that arrives as epoch seconds from v3 and as
// RFC 3339 from v1. Both decode; an absent or unparseable one is the zero
// value, which reads as unknown rather than as 1970.
type Timestamp struct {
	time.Time
}

func (t *Timestamp) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if secs, err := strconv.ParseFloat(string(data), 64); err == nil {
		t.Time = time.Unix(int64(secs), 0).UTC()
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, s); err == nil {
		t.Time = parsed.UTC()
	}
	return nil
}

// Resumable reports whether a session is young enough for Devin to continue.
// An unknown timestamp is treated as resumable: the caller learns otherwise
// from the refused send, which beats declaring a live session dead.
func (s *SessionStatus) Resumable() bool {
	if s.LastActivity().IsZero() {
		return true
	}
	return time.Since(s.LastActivity()) < resumeWindow
}

// LastActivity is the most recent of the two timestamps, since v1 and v3 differ
// on which they populate.
func (s *SessionStatus) LastActivity() time.Time {
	if s.UpdatedAt.After(s.CreatedAt.Time) {
		return s.UpdatedAt.Time
	}
	return s.CreatedAt.Time
}

// PullRequest contains PR information from a session.
type PullRequest struct {
	URL   string `json:"pr_url"`
	State string `json:"pr_state"`
}

// CreateSession creates a new Devin session with the given prompt.
func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.orgURL()+"/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("devin API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result CreateSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// SendMessageRequest is the payload for sending a message to an existing session.
type SendMessageRequest struct {
	Message string `json:"message"`
}

// SendMessage posts a follow-up message to an existing Devin session via the
// v3 organization-scoped messages endpoint:
//
//	POST /v3/organizations/{org_id}/sessions/{session_id}/messages
//
// This resumes a session that is waiting for user input and instructs Devin to
// continue working. Poll the session afterward with PollUntilDone to wait for
// the follow-up work to finish.
//
// See https://docs.devin.ai/api-reference/v3/sessions/post-organizations-session-messages
func (c *Client) SendMessage(ctx context.Context, sessionID, message string) error {
	body, err := json.Marshal(SendMessageRequest{Message: message})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := c.orgURL() + "/sessions/" + sessionID + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("devin API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetSession retrieves the current status of a Devin session, falling back to
// the v1 per-session endpoint when the organization-scoped read is refused.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionStatus, error) {
	endpoint := c.orgURL() + "/sessions/" + sessionID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, Endpoint: endpoint, Body: string(respBody)}
		if apiErr.Forbidden() {
			detail, v1Err := c.getSessionV1(ctx, sessionID)
			if v1Err != nil {
				return nil, fmt.Errorf("%w (v1 fallback also failed: %v)", apiErr, v1Err)
			}
			return detail.toStatus(), nil
		}
		return nil, apiErr
	}

	var result SessionStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// sessionDetailV1 is the v1 per-session response. It is scoped to what the API
// key can see rather than to the organization, so it answers for a session the
// key created even where the v3 organization reads are refused. It carries the
// two things the v3 reads are wanted for — the status and the structured
// output — plus the transcript.
//
//	GET /v1/sessions/{session_id}
//
// See https://docs.devin.ai/api-reference/v1/sessions/retrieve-details-about-an-existing-session
type sessionDetailV1 struct {
	SessionID        string          `json:"session_id"`
	Status           string          `json:"status"`
	StatusEnum       string          `json:"status_enum"`
	Title            string          `json:"title"`
	Tags             []string        `json:"tags"`
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	Messages         json.RawMessage `json:"messages,omitempty"`
	CreatedAt        Timestamp       `json:"created_at"`
	UpdatedAt        Timestamp       `json:"updated_at"`
	PullRequest      *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

// toStatus maps a v1 detail onto the status shape the poller and the formatters
// read. v1 reports the lifecycle in status_enum (working, blocked, expired,
// finished, ...) where v3 reports it in status, so the enum becomes the status
// and the v3-shaped detail field carries it too — the poller's terminal check
// looks at both, and dropping one would make a finished session read as still
// working.
func (d *sessionDetailV1) toStatus() *SessionStatus {
	status := d.StatusEnum
	if status == "" {
		status = d.Status
	}
	result := &SessionStatus{
		SessionID:    d.SessionID,
		Status:       status,
		StatusDetail: status,
		Title:        d.Title,
		URL:          "https://app.devin.ai/sessions/" + strings.TrimPrefix(d.SessionID, "devin-"),
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
	}
	if d.PullRequest != nil && d.PullRequest.URL != "" {
		result.PullRequests = []PullRequest{{URL: d.PullRequest.URL}}
	}
	return result
}

// getSessionV1 reads a session through the v1 per-session endpoint.
func (c *Client) getSessionV1(ctx context.Context, sessionID string) (*sessionDetailV1, error) {
	endpoint := v1BaseURL + "/sessions/" + sessionID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: endpoint, Body: string(respBody)}
	}

	var result sessionDetailV1
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetMessages retrieves the raw message history JSON for a Devin session
// via the v3 organization-scoped messages endpoint:
//
//	GET /v3/organizations/{org_id}/sessions/{session_id}/messages
//
// The entire JSON response is returned as-is so the calling agent can
// parse and interpret the conversation directly.
//
// See https://docs.devin.ai/api-reference/v3/sessions/get-organizations-session-messages
func (c *Client) GetMessages(ctx context.Context, sessionID string) (string, error) {
	url := c.orgURL() + "/sessions/" + sessionID + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("send request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		apiErr := &APIError{StatusCode: resp.StatusCode, Endpoint: url, Body: string(body)}
		if apiErr.Forbidden() {
			detail, v1Err := c.getSessionV1(ctx, sessionID)
			if v1Err != nil {
				return "", fmt.Errorf("%w (v1 fallback also failed: %v)", apiErr, v1Err)
			}
			if len(detail.Messages) == 0 {
				return "", fmt.Errorf("%w (v1 fallback returned no messages)", apiErr)
			}
			return string(detail.Messages), nil
		}
		return "", apiErr
	}

	if len(body) == 0 {
		return "", fmt.Errorf("devin API returned empty response (status %d) from %s", resp.StatusCode, url)
	}

	return string(body), nil
}

// SessionInsight contains enriched session data from the insights endpoint,
// including analysis with action items, issues, timeline, and classification.
type SessionInsight struct {
	SessionID        string           `json:"session_id"`
	Status           string           `json:"status"`
	StatusDetail     string           `json:"status_detail"`
	Title            string           `json:"title"`
	URL              string           `json:"url"`
	PullRequests     []PullRequest    `json:"pull_requests,omitempty"`
	IsArchived       bool             `json:"is_archived"`
	ACUsConsumed     float64          `json:"acus_consumed"`
	StructuredOutput json.RawMessage  `json:"structured_output,omitempty"`
	Analysis         *SessionAnalysis `json:"analysis,omitempty"`
}

// SessionAnalysis contains the AI-generated analysis of a session.
type SessionAnalysis struct {
	ActionItems    []string               `json:"action_items,omitempty"`
	Issues         []string               `json:"issues,omitempty"`
	Timeline       []string               `json:"timeline,omitempty"`
	Classification *SessionClassification `json:"classification,omitempty"`
}

// SessionClassification describes the category and technologies of a session.
type SessionClassification struct {
	Category             string   `json:"category"`
	Confidence           float64  `json:"confidence"`
	ProgrammingLanguages []string `json:"programming_languages,omitempty"`
	ToolsAndFrameworks   []string `json:"tools_and_frameworks,omitempty"`
}

// insightsResponse is the paginated response from the session insights endpoint.
type insightsResponse struct {
	Items       []SessionInsight `json:"items"`
	EndCursor   string           `json:"end_cursor"`
	HasNextPage bool             `json:"has_next_page"`
}

// GetSessionInsights retrieves enriched session data including analysis from
// the organization-scoped insights endpoint, filtered to a single session.
//
//	GET /v3/organizations/{org_id}/sessions/insights?session_ids={session_id}
func (c *Client) GetSessionInsights(ctx context.Context, sessionID string) (*SessionInsight, error) {
	url := c.orgURL() + "/sessions/insights?session_ids=" + sessionID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, Endpoint: url, Body: string(respBody)}
		if apiErr.Forbidden() {
			// Insights are how structured_output reaches a mission's routers, so
			// losing them to a permission is not cosmetic. v1 carries the same
			// field without Devin's analysis, which nothing routes on.
			detail, v1Err := c.getSessionV1(ctx, sessionID)
			if v1Err != nil {
				return nil, fmt.Errorf("%w (v1 fallback also failed: %v)", apiErr, v1Err)
			}
			status := detail.toStatus()
			return &SessionInsight{
				SessionID:        status.SessionID,
				Status:           status.Status,
				StatusDetail:     status.StatusDetail,
				Title:            status.Title,
				URL:              status.URL,
				PullRequests:     status.PullRequests,
				StructuredOutput: detail.StructuredOutput,
			}, nil
		}
		return nil, apiErr
	}

	var result insightsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("no insights found for session %s", sessionID)
	}

	return &result.Items[0], nil
}

// ArchiveSession archives a completed Devin session.
func (c *Client) ArchiveSession(ctx context.Context, sessionID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.orgURL()+"/sessions/"+sessionID+"/archive", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("devin API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SessionSummary is one entry from a session list response. It is deliberately
// thinner than SessionStatus: a list call is for finding the session you want,
// after which GetSession/GetMessages fetch its detail.
type SessionSummary struct {
	SessionID   string   `json:"session_id"`
	Status      string   `json:"status"`
	StatusEnum  string   `json:"status_enum"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

// listSessionsQuery builds the query string for a tag search. Each tag is a
// repeated `tags` parameter, which is how the endpoint expresses a list. A
// userEmail narrows the search to that creator's sessions.
func listSessionsQuery(tags []string, limit int, userEmail string) url.Values {
	if limit <= 0 {
		limit = defaultListLimit
	}
	query := url.Values{}
	for _, tag := range tags {
		query.Add("tags", tag)
	}
	if userEmail != "" {
		query.Set("user_email", userEmail)
	}
	query.Set("limit", strconv.Itoa(limit))
	return query
}

// listSessionsResponse is the envelope returned by the v1 list endpoint.
type listSessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

// ListSessionsByTags returns the organization's sessions carrying ALL of the
// given tags, most useful for finding the sessions an earlier mission run
// created for a ticket.
//
//	GET /v1/sessions?tags=<tag>&tags=<tag>&limit=<n>
//
// This is the v1 endpoint, not v3: v1 documents tag filtering directly, while
// the v3 list endpoint takes an undocumented `qs` query-params object. The
// organization is the one the API key belongs to, so orgID is not in the path.
//
// A userEmail narrows the search to the sessions that address created. That is
// the only lever where the unfiltered search is refused: unlike the per-session
// reads, no self-scoped list endpoint exists to fall back to.
//
// See https://docs.devin.ai/api-reference/v1/sessions/list-sessions
func (c *Client) ListSessionsByTags(ctx context.Context, tags []string, limit int, userEmail string) ([]SessionSummary, error) {
	if len(tags) == 0 {
		return nil, fmt.Errorf("at least one tag is required")
	}
	endpoint := v1BaseURL + "/sessions?" + listSessionsQuery(tags, limit, userEmail).Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, Endpoint: endpoint, Body: string(respBody)}
		if apiErr.Forbidden() {
			return nil, fmt.Errorf("%w — listing sessions is an organization-wide read (ViewOrgSessions); a refusal here says the history cannot be seen, NOT that the tag has no sessions", apiErr)
		}
		return nil, apiErr
	}

	var result listSessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result.Sessions, nil
}

// PollUntilDone polls the session status until it reaches a terminal state
// or the context is cancelled. It returns the final session status.
//
// Terminal conditions in the v3 API:
//
// Primary (by status):
//   - "exit": session ended
//   - "error": session encountered an error
//   - "suspended": session is suspended
//   - "sleeping": session finished and went to sleep
//
// Secondary (by status_detail while status is still "running"):
//   - "waiting_for_user": Devin finished its task and is waiting for follow-up
//   - "finished": task completed
//
// Where the v3 read is refused and GetSession answers from v1 instead, the
// status carries v1's status_enum, whose terminal values are named differently
// ("blocked" for waiting on a human, "expired" and "stopped" for ended). They
// are matched too: unrecognised terminal states are indistinguishable from
// still-working ones, so a finished session would poll until the timeout.
func (c *Client) PollUntilDone(ctx context.Context, sessionID string, pollInterval, pollTimeout time.Duration) (*SessionStatus, error) {
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	if pollTimeout == 0 {
		pollTimeout = defaultPollTimeout
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	timeout := time.After(pollTimeout)
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("polling timed out after %v for session %s", pollTimeout, sessionID)
		case <-ticker.C:
			status, err := c.GetSession(ctx, sessionID)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors >= maxPollErrors {
					return nil, fmt.Errorf("poll session %s: %d consecutive errors, last: %w", sessionID, consecutiveErrors, err)
				}
				// transient error, will retry on next tick
				continue
			}
			consecutiveErrors = 0

			// Primary terminal states (session is no longer running), v3 names
			// first and v1's status_enum names after.
			switch status.Status {
			case "exit", "error", "suspended", "sleeping", "waiting_for_user",
				"blocked", "expired", "stopped":
				return status, nil
			}

			// Secondary terminal: session is still "running" but Devin has
			// finished its task and is waiting for further instructions.
			// For Squadron's purposes this means the work is done.
			switch status.StatusDetail {
			case "waiting_for_user", "finished":
				return status, nil
			}
			// still working (new, claimed, running, resuming), continue polling
		}
	}
}
