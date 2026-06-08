package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	squadron "github.com/mlund01/squadron-sdk"
	"github.com/ericlakich/squadron-plugin-devin/devin"
)

// tools defines the metadata for all tools provided by this plugin.
var tools = map[string]*squadron.ToolInfo{
	"code_qa": {
		Name: "code_qa",
		Description: "Perform a full QA review of a pull request using Devin AI. " +
			"Devin will check out the PR, analyze the changes, run tests if applicable, " +
			"and return a comprehensive QA summary including potential issues, test coverage gaps, " +
			"and suggestions for improvement.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"pr_url": {
					Type:        squadron.TypeString,
					Description: "The full URL of the GitHub pull request to QA (e.g. https://github.com/org/repo/pull/123)",
				},
				"instructions": {
					Type:        squadron.TypeString,
					Description: "Optional additional instructions or focus areas for the QA review",
				},
			},
			Required: []string{"pr_url"},
		},
	},
	"code_review": {
		Name: "code_review",
		Description: "Perform a full code review of a pull request using Devin AI. " +
			"Devin will review the PR diff, leave inline comments on the GitHub PR, " +
			"and provide an overall review summary covering code quality, correctness, " +
			"security concerns, and adherence to best practices.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"pr_url": {
					Type:        squadron.TypeString,
					Description: "The full URL of the GitHub pull request to review (e.g. https://github.com/org/repo/pull/123)",
				},
				"instructions": {
					Type:        squadron.TypeString,
					Description: "Optional additional instructions or focus areas for the code review",
				},
			},
			Required: []string{"pr_url"},
		},
	},
	"code_develop": {
		Name: "code_develop",
		Description: "Develop code on a repository using Devin AI. " +
			"Devin will clone the repo, implement the requested changes, run tests, " +
			"and open a pull request with the completed work. " +
			"Use this for feature development, bug fixes, refactoring, or any code changes.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"repo_url": {
					Type:        squadron.TypeString,
					Description: "The full URL of the GitHub repository to develop on (e.g. https://github.com/org/repo)",
				},
				"task": {
					Type:        squadron.TypeString,
					Description: "A description of the development task to perform (e.g. 'Add pagination to the /users API endpoint')",
				},
				"branch": {
					Type:        squadron.TypeString,
					Description: "Optional branch name for Devin to create. If not specified, Devin will choose an appropriate name.",
				},
				"instructions": {
					Type:        squadron.TypeString,
					Description: "Optional additional context, constraints, or coding guidelines for the task",
				},
			},
			Required: []string{"repo_url", "task"},
		},
	},
	"check_session": {
		Name: "check_session",
		Description: "Check the status of an existing Devin session. " +
			"Returns the full session status including current state, pull requests, " +
			"and Devin's messages. Use this to inspect a session that was previously " +
			"created by another tool or to check on a long-running session.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"session_id": {
					Type:        squadron.TypeString,
					Description: "The Devin session ID (e.g. 32fee96e7997499ca010301aa50eefce)",
				},
			},
			Required: []string{"session_id"},
		},
	},
	"send_message": {
		Name: "send_message",
		Description: "Send a follow-up message to an existing Devin session and wait for " +
			"Devin to finish responding. Use this to continue a conversation with a session " +
			"that is waiting for user input, for example to answer a question, give additional " +
			"instructions, or request changes. The session must not be archived. " +
			"Returns Devin's response once it finishes the follow-up work.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"session_id": {
					Type:        squadron.TypeString,
					Description: "The Devin session ID to send the message to (e.g. 32fee96e7997499ca010301aa50eefce)",
				},
				"message": {
					Type:        squadron.TypeString,
					Description: "The message to send to Devin (e.g. a follow-up instruction, answer, or change request)",
				},
			},
			Required: []string{"session_id", "message"},
		},
	},
	"complete_session": {
		Name: "complete_session",
		Description: "Finalize and archive a Devin session. Call this upon mission finalization " +
			"once no further follow-up messages are needed, to archive the session and release its " +
			"resources. After completion the session can no longer be resumed with send_message.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"session_id": {
					Type:        squadron.TypeString,
					Description: "The Devin session ID to finalize and archive (e.g. 32fee96e7997499ca010301aa50eefce)",
				},
			},
			Required: []string{"session_id"},
		},
	},
}

// Plugin implements the squadron.ToolProvider interface for Devin AI integration.
type Plugin struct {
	client            *devin.Client
	pollTimeout       time.Duration
	archiveOnComplete bool
}

// Configure receives settings from the Squadron HCL config.
// Required settings:
//   - api_key: Devin AI service user API key (starts with cog_).
//   - org_id:  Devin organization ID.
//
// Optional settings:
//   - poll_timeout_minutes: Maximum time in minutes to wait for a Devin session
//     to complete. Defaults to 60.
//   - archive_on_complete: Whether the code_qa, code_review, and code_develop
//     tools archive their session once Devin finishes. Defaults to true. Set to
//     false to leave sessions in a resumable state so they can be continued with
//     send_message and explicitly finalized with complete_session.
func (p *Plugin) Configure(settings map[string]string) error {
	apiKey, ok := settings["api_key"]
	if !ok || apiKey == "" {
		return fmt.Errorf("missing required setting: api_key")
	}

	orgID, ok := settings["org_id"]
	if !ok || orgID == "" {
		return fmt.Errorf("missing required setting: org_id")
	}

	p.client = devin.NewClient(apiKey, orgID)

	p.pollTimeout = 60 * time.Minute
	if v, ok := settings["poll_timeout_minutes"]; ok && v != "" {
		minutes, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid poll_timeout_minutes %q: %w", v, err)
		}
		if minutes < 1 {
			return fmt.Errorf("poll_timeout_minutes must be at least 1, got %d", minutes)
		}
		p.pollTimeout = time.Duration(minutes) * time.Minute
	}

	p.archiveOnComplete = true
	if v, ok := settings["archive_on_complete"]; ok && v != "" {
		archive, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid archive_on_complete %q: %w", v, err)
		}
		p.archiveOnComplete = archive
	}

	return nil
}

// finalizeSession archives a completed session unless the plugin is configured
// to leave sessions resumable (archive_on_complete = false). When archiving is
// skipped the session stays open so it can be continued with send_message and
// explicitly archived later with complete_session.
func (p *Plugin) finalizeSession(ctx context.Context, sessionID string) {
	if p.archiveOnComplete {
		p.client.ArchiveSession(ctx, sessionID)
	}
}

// Call dispatches a tool invocation to the appropriate handler.
func (p *Plugin) Call(ctx context.Context, toolName string, payload string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("plugin not configured: call Configure first")
	}

	switch toolName {
	case "code_qa":
		return p.callCodeQA(ctx, payload)
	case "code_review":
		return p.callCodeReview(ctx, payload)
	case "code_develop":
		return p.callCodeDevelop(ctx, payload)
	case "check_session":
		return p.callCheckSession(ctx, payload)
	case "send_message":
		return p.callSendMessage(ctx, payload)
	case "complete_session":
		return p.callCompleteSession(ctx, payload)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

// GetToolInfo returns metadata for a specific tool.
func (p *Plugin) GetToolInfo(toolName string) (*squadron.ToolInfo, error) {
	info, ok := tools[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
	return info, nil
}

// ListTools returns metadata for all tools provided by this plugin.
func (p *Plugin) ListTools() ([]*squadron.ToolInfo, error) {
	result := make([]*squadron.ToolInfo, 0, len(tools))
	for _, info := range tools {
		result = append(result, info)
	}
	return result, nil
}

// codeQAParams are the parameters for the code_qa tool.
type codeQAParams struct {
	PRURL        string `json:"pr_url"`
	Instructions string `json:"instructions,omitempty"`
}

// codeReviewParams are the parameters for the code_review tool.
type codeReviewParams struct {
	PRURL        string `json:"pr_url"`
	Instructions string `json:"instructions,omitempty"`
}

// codeDevelopParams are the parameters for the code_develop tool.
type codeDevelopParams struct {
	RepoURL      string `json:"repo_url"`
	Task         string `json:"task"`
	Branch       string `json:"branch,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// checkSessionParams are the parameters for the check_session tool.
type checkSessionParams struct {
	SessionID string `json:"session_id"`
}

// sendMessageParams are the parameters for the send_message tool.
type sendMessageParams struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// completeSessionParams are the parameters for the complete_session tool.
type completeSessionParams struct {
	SessionID string `json:"session_id"`
}

// callCodeQA creates a Devin session to perform QA on a PR and polls until completion.
func (p *Plugin) callCodeQA(ctx context.Context, payload string) (string, error) {
	var params codeQAParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if params.PRURL == "" {
		return "", fmt.Errorf("pr_url is required")
	}

	prompt := buildQAPrompt(params.PRURL, params.Instructions)

	session, err := p.client.CreateSession(ctx, devin.CreateSessionRequest{
		Prompt: prompt,
	})
	if err != nil {
		return "", fmt.Errorf("create devin session: %w", err)
	}

	status, err := p.client.PollUntilDone(ctx, session.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", session.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, session.SessionID)

	p.finalizeSession(ctx, session.SessionID)

	return formatQAResult(session.SessionID, session.URL, status, messages, msgErr), nil
}

// callCodeReview creates a Devin session to review a PR and polls until completion.
func (p *Plugin) callCodeReview(ctx context.Context, payload string) (string, error) {
	var params codeReviewParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if params.PRURL == "" {
		return "", fmt.Errorf("pr_url is required")
	}

	prompt := buildReviewPrompt(params.PRURL, params.Instructions)

	session, err := p.client.CreateSession(ctx, devin.CreateSessionRequest{
		Prompt: prompt,
	})
	if err != nil {
		return "", fmt.Errorf("create devin session: %w", err)
	}

	status, err := p.client.PollUntilDone(ctx, session.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", session.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, session.SessionID)

	p.finalizeSession(ctx, session.SessionID)

	return formatReviewResult(session.SessionID, session.URL, status, messages, msgErr), nil
}

// callCodeDevelop creates a Devin session to develop code on a repo and polls until completion.
func (p *Plugin) callCodeDevelop(ctx context.Context, payload string) (string, error) {
	var params codeDevelopParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if params.RepoURL == "" {
		return "", fmt.Errorf("repo_url is required")
	}
	if params.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	prompt := buildDevelopPrompt(params.Task, params.Branch, params.Instructions)

	session, err := p.client.CreateSession(ctx, devin.CreateSessionRequest{
		Prompt: prompt,
		Repos:  []string{params.RepoURL},
	})
	if err != nil {
		return "", fmt.Errorf("create devin session: %w", err)
	}

	status, err := p.client.PollUntilDone(ctx, session.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", session.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, session.SessionID)

	p.finalizeSession(ctx, session.SessionID)

	return formatDevelopResult(session.SessionID, session.URL, status, messages, msgErr), nil
}

// callCheckSession retrieves the full status, messages, and insights for an existing Devin session.
func (p *Plugin) callCheckSession(ctx context.Context, payload string) (string, error) {
	var params checkSessionParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if params.SessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	status, err := p.client.GetSession(ctx, params.SessionID)
	if err != nil {
		return "", fmt.Errorf("get session %s: %w", params.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, params.SessionID)

	// Insights are best-effort; they may not be available for all sessions.
	insights, _ := p.client.GetSessionInsights(ctx, params.SessionID)

	return formatCheckSessionResult(params.SessionID, status, messages, msgErr, insights), nil
}

// callSendMessage sends a follow-up message to an existing session and polls
// until Devin finishes responding, then returns Devin's updated messages.
func (p *Plugin) callSendMessage(ctx context.Context, payload string) (string, error) {
	var params sendMessageParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if params.SessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if params.Message == "" {
		return "", fmt.Errorf("message is required")
	}

	if err := p.client.SendMessage(ctx, params.SessionID, params.Message); err != nil {
		return "", fmt.Errorf("send message to session %s: %w", params.SessionID, err)
	}

	status, err := p.client.PollUntilDone(ctx, params.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", params.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, params.SessionID)

	return formatSendMessageResult(params.SessionID, status, messages, msgErr), nil
}

// callCompleteSession finalizes a session by archiving it. This is the explicit
// counterpart to leaving sessions resumable (archive_on_complete = false): once
// the mission is finished and no further follow-ups are needed, the session is
// archived to release its resources.
func (p *Plugin) callCompleteSession(ctx context.Context, payload string) (string, error) {
	var params completeSessionParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if params.SessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	if err := p.client.ArchiveSession(ctx, params.SessionID); err != nil {
		return "", fmt.Errorf("archive session %s: %w", params.SessionID, err)
	}

	var b strings.Builder
	b.WriteString("=== Devin Session Finalized ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", params.SessionID))
	b.WriteString("Status: archived\n\n")
	b.WriteString("The session has been finalized and archived. It can no longer be resumed.\n")
	return b.String(), nil
}

// buildQAPrompt constructs the Devin prompt for a QA review.
func buildQAPrompt(prURL string, instructions string) string {
	var b strings.Builder
	b.WriteString("Perform a thorough QA review of this pull request: ")
	b.WriteString(prURL)
	b.WriteString("\n\n")
	b.WriteString("Your QA review should include:\n")
	b.WriteString("1. Check out the PR branch and understand the changes\n")
	b.WriteString("2. Identify potential bugs, edge cases, or logic errors\n")
	b.WriteString("3. Verify error handling is adequate\n")
	b.WriteString("4. Run any existing tests and note failures\n")
	b.WriteString("5. Identify missing test coverage for new or changed code\n")
	b.WriteString("6. Check for regressions in related functionality\n")
	b.WriteString("7. Verify the changes match the PR description and any linked issues\n")
	b.WriteString("8. Note any performance concerns\n\n")
	b.WriteString("Provide a detailed summary of your findings with clear categorization ")
	b.WriteString("(critical issues, warnings, suggestions, and things that look good).\n")

	if instructions != "" {
		b.WriteString("\nAdditional instructions: ")
		b.WriteString(instructions)
		b.WriteString("\n")
	}

	return b.String()
}

// buildReviewPrompt constructs the Devin prompt for a code review.
func buildReviewPrompt(prURL string, instructions string) string {
	var b strings.Builder
	b.WriteString("Perform a thorough code review of this pull request: ")
	b.WriteString(prURL)
	b.WriteString("\n\n")
	b.WriteString("Your code review should:\n")
	b.WriteString("1. Review every changed file in the PR diff\n")
	b.WriteString("2. Leave inline review comments directly on the GitHub PR for specific issues\n")
	b.WriteString("3. Evaluate code quality, readability, and maintainability\n")
	b.WriteString("4. Check for correctness and potential bugs\n")
	b.WriteString("5. Identify security concerns or vulnerabilities\n")
	b.WriteString("6. Verify adherence to best practices and coding conventions\n")
	b.WriteString("7. Suggest improvements where appropriate\n")
	b.WriteString("8. Submit your review on GitHub with an overall summary comment\n\n")
	b.WriteString("After completing the review, provide a summary of your findings.\n")

	if instructions != "" {
		b.WriteString("\nAdditional instructions: ")
		b.WriteString(instructions)
		b.WriteString("\n")
	}

	return b.String()
}

// buildDevelopPrompt constructs the Devin prompt for a development task.
func buildDevelopPrompt(task, branch, instructions string) string {
	var b strings.Builder
	b.WriteString("Task: ")
	b.WriteString(task)
	b.WriteString("\n\n")
	b.WriteString("Please complete this development task by following these steps:\n")
	b.WriteString("1. Understand the existing codebase structure\n")
	b.WriteString("2. Create a new branch for your changes")
	if branch != "" {
		b.WriteString(" named: ")
		b.WriteString(branch)
	}
	b.WriteString("\n")
	b.WriteString("3. Implement the requested changes with clean, well-structured code\n")
	b.WriteString("4. Follow existing code conventions and patterns in the repository\n")
	b.WriteString("5. Add or update tests to cover the changes\n")
	b.WriteString("6. Run the existing test suite and ensure all tests pass\n")
	b.WriteString("7. Commit your changes with clear, descriptive commit messages\n")
	b.WriteString("8. Open a pull request with a detailed description of the changes\n\n")

	if instructions != "" {
		b.WriteString("Additional instructions: ")
		b.WriteString(instructions)
		b.WriteString("\n\n")
	}

	b.WriteString("When complete, provide a summary of the changes made and the PR link.\n")

	return b.String()
}

// formatPullRequests formats a list of pull requests into a readable string.
func formatPullRequests(prs []devin.PullRequest) string {
	var b strings.Builder
	for _, pr := range prs {
		if pr.URL != "" {
			b.WriteString(fmt.Sprintf("  %s", pr.URL))
			if pr.State != "" {
				b.WriteString(fmt.Sprintf(" (%s)", pr.State))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// formatDevinResponse writes the "--- Devin's Response ---" section with
// the raw messages JSON payload or an appropriate fallback.
func formatDevinResponse(b *strings.Builder, sessionURL string, messagesJSON string, msgErr error) {
	b.WriteString("--- Devin's Response ---\n\n")
	if msgErr != nil {
		b.WriteString("Devin returned an error in messaging. Review this session at ")
		b.WriteString(sessionURL)
		b.WriteString("\nError detail: ")
		b.WriteString(msgErr.Error())
		b.WriteString("\n")
	} else if messagesJSON != "" {
		b.WriteString(messagesJSON)
		b.WriteString("\n")
	} else {
		b.WriteString("Devin did not return a message. Continue to the next task.\n")
	}
}

// formatQAResult formats the QA session result into a readable text summary.
func formatQAResult(sessionID, sessionURL string, status *devin.SessionStatus, messagesJSON string, msgErr error) string {
	var b strings.Builder
	b.WriteString("=== Devin QA Review Complete ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	b.WriteString(fmt.Sprintf("URL: %s\n", sessionURL))
	b.WriteString(fmt.Sprintf("Status: %s\n", status.Status))
	if status.StatusDetail != "" {
		b.WriteString(fmt.Sprintf("Status Detail: %s\n", status.StatusDetail))
	}
	b.WriteString("\n")

	if status.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n\n", status.Title))
	}

	formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nView the full Devin session for detailed findings: ")
	b.WriteString(sessionURL)
	b.WriteString("\n")

	return b.String()
}

// formatReviewResult formats the code review session result into a readable text summary.
func formatReviewResult(sessionID, sessionURL string, status *devin.SessionStatus, messagesJSON string, msgErr error) string {
	var b strings.Builder
	b.WriteString("=== Devin Code Review Complete ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	b.WriteString(fmt.Sprintf("URL: %s\n", sessionURL))
	b.WriteString(fmt.Sprintf("Status: %s\n", status.Status))
	if status.StatusDetail != "" {
		b.WriteString(fmt.Sprintf("Status Detail: %s\n", status.StatusDetail))
	}
	b.WriteString("\n")

	if status.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n\n", status.Title))
	}

	if prs := formatPullRequests(status.PullRequests); prs != "" {
		b.WriteString("Pull Requests:\n")
		b.WriteString(prs)
		b.WriteString("\n")
	}

	formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nReview comments have been posted directly on the GitHub PR.\n")
	b.WriteString("View the full Devin session: ")
	b.WriteString(sessionURL)
	b.WriteString("\n")

	return b.String()
}

// formatDevelopResult formats the development session result into a readable text summary.
func formatDevelopResult(sessionID, sessionURL string, status *devin.SessionStatus, messagesJSON string, msgErr error) string {
	var b strings.Builder
	b.WriteString("=== Devin Development Complete ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	b.WriteString(fmt.Sprintf("URL: %s\n", sessionURL))
	b.WriteString(fmt.Sprintf("Status: %s\n", status.Status))
	if status.StatusDetail != "" {
		b.WriteString(fmt.Sprintf("Status Detail: %s\n", status.StatusDetail))
	}
	b.WriteString("\n")

	if status.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n\n", status.Title))
	}

	if prs := formatPullRequests(status.PullRequests); prs != "" {
		b.WriteString("Pull Requests:\n")
		b.WriteString(prs)
		b.WriteString("\n")
	}

	formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nView the full Devin session for details: ")
	b.WriteString(sessionURL)
	b.WriteString("\n")

	return b.String()
}

// formatSendMessageResult formats the result of a follow-up message into a readable text summary.
func formatSendMessageResult(sessionID string, status *devin.SessionStatus, messagesJSON string, msgErr error) string {
	var b strings.Builder
	b.WriteString("=== Devin Follow-up Complete ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	if status.URL != "" {
		b.WriteString(fmt.Sprintf("URL: %s\n", status.URL))
	}
	b.WriteString(fmt.Sprintf("Status: %s\n", status.Status))
	if status.StatusDetail != "" {
		b.WriteString(fmt.Sprintf("Status Detail: %s\n", status.StatusDetail))
	}
	b.WriteString("\n")

	if status.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n\n", status.Title))
	}

	if prs := formatPullRequests(status.PullRequests); prs != "" {
		b.WriteString("Pull Requests:\n")
		b.WriteString(prs)
		b.WriteString("\n")
	}

	sessionURL := status.URL
	if sessionURL == "" {
		sessionURL = "https://app.devin.ai/sessions/" + sessionID
	}
	formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nThe session is still open. Send another message to continue, ")
	b.WriteString("or call complete_session to finalize it.\n")

	return b.String()
}

// formatCheckSessionResult formats a session status check into a readable text summary.
func formatCheckSessionResult(sessionID string, status *devin.SessionStatus, messagesJSON string, msgErr error, insights *devin.SessionInsight) string {
	var b strings.Builder
	b.WriteString("=== Devin Session Status ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	if status.URL != "" {
		b.WriteString(fmt.Sprintf("URL: %s\n", status.URL))
	}
	b.WriteString(fmt.Sprintf("Status: %s\n", status.Status))
	if status.StatusDetail != "" {
		b.WriteString(fmt.Sprintf("Status Detail: %s\n", status.StatusDetail))
	}
	b.WriteString(fmt.Sprintf("Archived: %v\n", status.IsArchived))
	b.WriteString("\n")

	if status.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n\n", status.Title))
	}

	if prs := formatPullRequests(status.PullRequests); prs != "" {
		b.WriteString("Pull Requests:\n")
		b.WriteString(prs)
		b.WriteString("\n")
	}

	sessionURL := status.URL
	if sessionURL == "" {
		sessionURL = "https://app.devin.ai/sessions/" + sessionID
	}
	formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	if insights != nil {
		formatInsights(&b, insights)
	}

	return b.String()
}

// formatInsights renders session insights analysis into the output.
func formatInsights(b *strings.Builder, insight *devin.SessionInsight) {
	b.WriteString("\n--- Session Insights ---\n\n")

	if insight.ACUsConsumed > 0 {
		b.WriteString(fmt.Sprintf("ACUs Consumed: %.2f\n", insight.ACUsConsumed))
	}

	if insight.Analysis != nil {
		a := insight.Analysis

		if a.Classification != nil && a.Classification.Category != "" {
			b.WriteString(fmt.Sprintf("Category: %s\n", a.Classification.Category))
			if len(a.Classification.ProgrammingLanguages) > 0 {
				b.WriteString(fmt.Sprintf("Languages: %s\n", strings.Join(a.Classification.ProgrammingLanguages, ", ")))
			}
			if len(a.Classification.ToolsAndFrameworks) > 0 {
				b.WriteString(fmt.Sprintf("Tools/Frameworks: %s\n", strings.Join(a.Classification.ToolsAndFrameworks, ", ")))
			}
		}

		if len(a.Issues) > 0 {
			b.WriteString("\nIssues:\n")
			for _, issue := range a.Issues {
				b.WriteString(fmt.Sprintf("  - %s\n", issue))
			}
		}

		if len(a.ActionItems) > 0 {
			b.WriteString("\nAction Items:\n")
			for _, item := range a.ActionItems {
				b.WriteString(fmt.Sprintf("  - %s\n", item))
			}
		}

		if len(a.Timeline) > 0 {
			b.WriteString("\nTimeline:\n")
			for _, entry := range a.Timeline {
				b.WriteString(fmt.Sprintf("  - %s\n", entry))
			}
		}
	}

	if len(insight.StructuredOutput) > 0 && string(insight.StructuredOutput) != "{}" && string(insight.StructuredOutput) != "null" {
		b.WriteString(fmt.Sprintf("\nStructured Output: %s\n", string(insight.StructuredOutput)))
	}
}
