// Package devin provides an HTTP client for the Devin AI v3 API.
package devin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	baseURL = "https://api.devin.ai/v3"

	// Polling configuration
	defaultPollInterval = 15 * time.Second
	defaultPollTimeout  = 60 * time.Minute

	// maxPollErrors is the number of consecutive transient errors tolerated
	// during polling before giving up.
	maxPollErrors = 5
)

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

// GetSession retrieves the current status of a Devin session.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionStatus, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.orgURL()+"/sessions/"+sessionID, nil)
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
		return nil, fmt.Errorf("devin API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result SessionStatus
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
		return "", fmt.Errorf("devin API error (status %d): %s", resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return "", fmt.Errorf("devin API returned empty response (status %d) from %s", resp.StatusCode, url)
	}

	return string(body), nil
}

// SessionInsight contains enriched session data from the insights endpoint,
// including analysis with action items, issues, timeline, and classification.
type SessionInsight struct {
	SessionID        string            `json:"session_id"`
	Status           string            `json:"status"`
	StatusDetail     string            `json:"status_detail"`
	Title            string            `json:"title"`
	URL              string            `json:"url"`
	PullRequests     []PullRequest     `json:"pull_requests,omitempty"`
	IsArchived       bool              `json:"is_archived"`
	ACUsConsumed     float64           `json:"acus_consumed"`
	StructuredOutput json.RawMessage   `json:"structured_output,omitempty"`
	Analysis         *SessionAnalysis  `json:"analysis,omitempty"`
}

// SessionAnalysis contains the AI-generated analysis of a session.
type SessionAnalysis struct {
	ActionItems    []string                `json:"action_items,omitempty"`
	Issues         []string                `json:"issues,omitempty"`
	Timeline       []string                `json:"timeline,omitempty"`
	Classification *SessionClassification  `json:"classification,omitempty"`
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
		return nil, fmt.Errorf("devin API error (status %d): %s", resp.StatusCode, string(respBody))
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

			// Primary terminal states (session is no longer running)
			switch status.Status {
			case "exit", "error", "suspended", "sleeping", "waiting_for_user":
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
