package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FedTax/squadron-plugin-devin/devin"
	squadron "github.com/mlund01/squadron-sdk"
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
				"title": titleProperty,
				"tags":  tagsProperty,
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
				"title": titleProperty,
				"tags":  tagsProperty,
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
				"title": titleProperty,
				"tags":  tagsProperty,
				"prompt_mode": {
					Type: squadron.TypeString,
					Description: "How to build the session prompt. \"default\" (the default) wraps the task in the " +
						"standard development workflow: create a branch, add tests, commit, open a PR. " +
						"\"raw\" sends task and instructions verbatim with no added steps — use it when those " +
						"steps would be wrong, e.g. a read-only investigation, or work that must continue on an " +
						"existing branch and PR.",
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
	"find_sessions": {
		Name: "find_sessions",
		Description: "Find existing Devin sessions by tag, so a workflow can discover what has already " +
			"been done for a ticket instead of being handed session ids. Returns one line per " +
			"matching session — id, status, title, PR link, timestamps — for the sessions " +
			"carrying ALL of the given tags. Pass the resulting session id to check_session for " +
			"detail, or to send_message to continue that session.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"tags": {
					Type: squadron.TypeArray,
					Description: "Tags a session must ALL carry to match (e.g. [\"DEV-8126\"] for every " +
						"session on a ticket, or [\"DEV-8126\", \"rate-investigation\"] for that " +
						"ticket's investigation only). Tags are matched exactly, so they must be the " +
						"tags the sessions were created with.",
					Items: &squadron.Property{
						Type: squadron.TypeString,
					},
				},
				"limit": {
					Type:        squadron.TypeNumber,
					Description: "Maximum number of sessions to return. Defaults to 20.",
				},
			},
			Required: []string{"tags"},
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

// titleProperty and tagsProperty are shared by every tool that creates a Devin
// session, so sessions can be found and grouped in the Devin UI by the mission
// or stage that created them.
var (
	titleProperty = squadron.Property{
		Type:        squadron.TypeString,
		Description: "Optional title for the Devin session (e.g. \"DEV-8126 investigate\"). Devin generates one if omitted.",
	}

	tagsProperty = squadron.Property{
		Type:        squadron.TypeArray,
		Description: "Optional tags to apply to the Devin session (e.g. [\"ratevariant\", \"investigate\", \"DEV-8126\"]), for filtering sessions later.",
		Items: &squadron.Property{
			Type: squadron.TypeString,
		},
	}
)

// Plugin implements the squadron.ToolProvider interface for Devin AI integration.
type Plugin struct {
	client            *devin.Client
	pollTimeout       time.Duration
	archiveOnComplete bool
	rawMessages       bool
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
//   - raw_messages: Whether tool results carry the session's entire messages
//     JSON payload. Defaults to false, in which case results carry Devin's final
//     message, its structured output, and PR links — the parts a caller acts on.
//     Set to true to get the full transcript back (large: it can dominate the
//     caller's context window).
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

	p.rawMessages = false
	if v, ok := settings["raw_messages"]; ok && v != "" {
		raw, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid raw_messages %q: %w", v, err)
		}
		p.rawMessages = raw
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
	case "find_sessions":
		return p.callFindSessions(ctx, payload)
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
	PRURL        string   `json:"pr_url"`
	Instructions string   `json:"instructions,omitempty"`
	Title        string   `json:"title,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// codeReviewParams are the parameters for the code_review tool.
type codeReviewParams struct {
	PRURL        string   `json:"pr_url"`
	Instructions string   `json:"instructions,omitempty"`
	Title        string   `json:"title,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// codeDevelopParams are the parameters for the code_develop tool.
type codeDevelopParams struct {
	RepoURL      string   `json:"repo_url"`
	Task         string   `json:"task"`
	Branch       string   `json:"branch,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Title        string   `json:"title,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	PromptMode   string   `json:"prompt_mode,omitempty"`
}

// promptModeRaw sends the caller's task and instructions to Devin verbatim,
// without the branch/tests/commit/PR workflow the default mode appends.
const promptModeRaw = "raw"

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

// findSessionsParams are the parameters for the find_sessions tool.
type findSessionsParams struct {
	Tags  []string `json:"tags"`
	Limit int      `json:"limit,omitempty"`
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
		Title:  params.Title,
		Tags:   params.Tags,
	})
	if err != nil {
		return "", fmt.Errorf("create devin session: %w", err)
	}

	status, err := p.client.PollUntilDone(ctx, session.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", session.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, session.SessionID)
	insights, _ := p.client.GetSessionInsights(ctx, session.SessionID)

	p.finalizeSession(ctx, session.SessionID)

	return p.formatQAResult(session.SessionID, session.URL, status, messages, msgErr, insights), nil
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
		Title:  params.Title,
		Tags:   params.Tags,
	})
	if err != nil {
		return "", fmt.Errorf("create devin session: %w", err)
	}

	status, err := p.client.PollUntilDone(ctx, session.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", session.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, session.SessionID)
	insights, _ := p.client.GetSessionInsights(ctx, session.SessionID)

	p.finalizeSession(ctx, session.SessionID)

	return p.formatReviewResult(session.SessionID, session.URL, status, messages, msgErr, insights), nil
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

	if params.PromptMode != "" && params.PromptMode != promptModeRaw && params.PromptMode != "default" {
		return "", fmt.Errorf("invalid prompt_mode %q: expected \"default\" or \"raw\"", params.PromptMode)
	}

	prompt := buildDevelopPrompt(params.Task, params.Branch, params.Instructions, params.PromptMode == promptModeRaw)

	session, err := p.client.CreateSession(ctx, devin.CreateSessionRequest{
		Prompt: prompt,
		Repos:  []string{params.RepoURL},
		Title:  params.Title,
		Tags:   params.Tags,
	})
	if err != nil {
		return "", fmt.Errorf("create devin session: %w", err)
	}

	status, err := p.client.PollUntilDone(ctx, session.SessionID, 0, p.pollTimeout)
	if err != nil {
		return "", fmt.Errorf("waiting for devin session %s: %w", session.SessionID, err)
	}

	messages, msgErr := p.client.GetMessages(ctx, session.SessionID)
	insights, _ := p.client.GetSessionInsights(ctx, session.SessionID)

	p.finalizeSession(ctx, session.SessionID)

	return p.formatDevelopResult(session.SessionID, session.URL, status, messages, msgErr, insights), nil
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

	return p.formatCheckSessionResult(params.SessionID, status, messages, msgErr, insights), nil
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

	insights, _ := p.client.GetSessionInsights(ctx, params.SessionID)

	return p.formatSendMessageResult(params.SessionID, status, messages, msgErr, insights), nil
}

// callFindSessions lists the sessions carrying every given tag, so a caller can
// discover prior work on a ticket rather than being passed session ids.
func (p *Plugin) callFindSessions(ctx context.Context, payload string) (string, error) {
	var params findSessionsParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if len(params.Tags) == 0 {
		return "", fmt.Errorf("tags is required and must contain at least one tag")
	}

	sessions, err := p.client.ListSessionsByTags(ctx, params.Tags, params.Limit)
	if err != nil {
		return "", fmt.Errorf("find sessions by tags %v: %w", params.Tags, err)
	}

	return formatFindSessionsResult(params.Tags, sessions), nil
}

// formatFindSessionsResult renders one line per session. An empty result is a
// real answer — "no session exists for this ticket yet" — not an error, so it
// says so explicitly rather than returning an empty body the caller has to
// guess about.
func formatFindSessionsResult(tags []string, sessions []devin.SessionSummary) string {
	var b strings.Builder
	b.WriteString("=== Devin Sessions ===\n\n")
	b.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(tags, ", ")))
	b.WriteString(fmt.Sprintf("Matches: %d\n\n", len(sessions)))

	if len(sessions) == 0 {
		b.WriteString("No session carries all of those tags. Nothing has been started for them yet,\n")
		b.WriteString("or the sessions that were started carry different tags.\n")
		return b.String()
	}

	for _, s := range sessions {
		status := s.Status
		if s.StatusEnum != "" {
			status = fmt.Sprintf("%s (%s)", status, s.StatusEnum)
		}
		b.WriteString(fmt.Sprintf("- %s — %s\n", s.SessionID, status))
		if s.Title != "" {
			b.WriteString(fmt.Sprintf("    title: %s\n", s.Title))
		}
		if s.PullRequest != nil && s.PullRequest.URL != "" {
			b.WriteString(fmt.Sprintf("    pr: %s\n", s.PullRequest.URL))
		}
		if s.CreatedAt != "" {
			b.WriteString(fmt.Sprintf("    created: %s\n", s.CreatedAt))
		}
		if s.UpdatedAt != "" {
			b.WriteString(fmt.Sprintf("    updated: %s\n", s.UpdatedAt))
		}
		if len(s.Tags) > 0 {
			b.WriteString(fmt.Sprintf("    tags: %s\n", strings.Join(s.Tags, ", ")))
		}
		b.WriteString(fmt.Sprintf("    url: https://app.devin.ai/sessions/%s\n", s.SessionID))
	}

	b.WriteString("\nUse check_session for a session's detail, or send_message to continue it.\n")
	return b.String()
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

// buildDevelopPrompt constructs the Devin prompt for a development task. In raw
// mode the task and instructions are passed through untouched: the branch /
// tests / commit / PR workflow below is wrong for a read-only investigation, and
// for work that must continue on a branch and PR that already exist.
func buildDevelopPrompt(task, branch, instructions string, raw bool) string {
	if raw {
		var b strings.Builder
		b.WriteString(task)
		if branch != "" {
			b.WriteString("\n\nBranch: ")
			b.WriteString(branch)
		}
		if instructions != "" {
			b.WriteString("\n\n")
			b.WriteString(instructions)
		}
		b.WriteString("\n")
		return b.String()
	}

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

// formatDevinResponse writes the "--- Devin's Response ---" section. By default
// it carries Devin's final message only; with raw_messages = true it carries the
// entire messages JSON payload, which is large enough to crowd out everything
// else in the caller's context.
func (p *Plugin) formatDevinResponse(b *strings.Builder, sessionURL string, messagesJSON string, msgErr error) {
	b.WriteString("--- Devin's Response ---\n\n")
	switch {
	case msgErr != nil:
		b.WriteString("Devin returned an error in messaging. Review this session at ")
		b.WriteString(sessionURL)
		b.WriteString("\nError detail: ")
		b.WriteString(msgErr.Error())
		b.WriteString("\n")
	case messagesJSON == "":
		b.WriteString("Devin did not return a message. Continue to the next task.\n")
	case p.rawMessages:
		b.WriteString(messagesJSON)
		b.WriteString("\n")
	default:
		last, ok := lastDevinMessage(messagesJSON)
		if !ok {
			// Unrecognised payload shape: fall back to the raw JSON rather than
			// silently dropping what Devin said.
			b.WriteString(messagesJSON)
			b.WriteString("\n")
			return
		}
		if last == "" {
			b.WriteString("Devin did not return a message. Continue to the next task.\n")
			return
		}
		b.WriteString(last)
		b.WriteString("\n\nFull transcript: ")
		b.WriteString(sessionURL)
		b.WriteString("\n")
	}
}

// lastDevinMessage extracts Devin's final message from the messages endpoint
// payload. It reports false when the payload doesn't parse into a recognised
// shape, so the caller can fall back to returning it verbatim.
func lastDevinMessage(messagesJSON string) (string, bool) {
	var entries []map[string]any

	if err := json.Unmarshal([]byte(messagesJSON), &entries); err != nil {
		var wrapper struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.Unmarshal([]byte(messagesJSON), &wrapper); err != nil || wrapper.Messages == nil {
			return "", false
		}
		entries = wrapper.Messages
	}

	// Walk backwards for the last entry Devin authored; a session ends with
	// Devin's summary unless it stopped to ask something.
	for i := len(entries) - 1; i >= 0; i-- {
		if !isDevinAuthored(entries[i]) {
			continue
		}
		if text := messageText(entries[i]); text != "" {
			return text, true
		}
	}

	return "", true
}

// isDevinAuthored reports whether a message entry came from Devin rather than
// from the caller. Entries without any type/origin field are assumed to be
// Devin's, since the alternative is dropping the response entirely.
func isDevinAuthored(entry map[string]any) bool {
	for _, key := range []string{"type", "origin", "role", "source", "author"} {
		v, ok := entry[key].(string)
		if !ok || v == "" {
			continue
		}
		v = strings.ToLower(v)
		if strings.Contains(v, "devin") || strings.Contains(v, "assistant") || strings.Contains(v, "agent") {
			return true
		}
		if strings.Contains(v, "user") || strings.Contains(v, "human") {
			return false
		}
	}
	return true
}

// messageText pulls the human-readable body out of a message entry, whichever
// field name the API used for it.
func messageText(entry map[string]any) string {
	for _, key := range []string{"message", "text", "content", "body"} {
		if v, ok := entry[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// formatStructuredOutput writes the session's structured output, which is the
// part a mission routes on — so it is included in every result, not only in
// check_session.
func formatStructuredOutput(b *strings.Builder, insight *devin.SessionInsight) {
	if insight == nil || len(insight.StructuredOutput) == 0 {
		return
	}
	if s := string(insight.StructuredOutput); s != "{}" && s != "null" {
		b.WriteString("--- Structured Output ---\n\n")
		b.WriteString(s)
		b.WriteString("\n\n")
	}
}

// formatQAResult formats the QA session result into a readable text summary.
func (p *Plugin) formatQAResult(sessionID, sessionURL string, status *devin.SessionStatus, messagesJSON string, msgErr error, insights *devin.SessionInsight) string {
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

	formatStructuredOutput(&b, insights)

	p.formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nView the full Devin session for detailed findings: ")
	b.WriteString(sessionURL)
	b.WriteString("\n")

	return b.String()
}

// formatReviewResult formats the code review session result into a readable text summary.
func (p *Plugin) formatReviewResult(sessionID, sessionURL string, status *devin.SessionStatus, messagesJSON string, msgErr error, insights *devin.SessionInsight) string {
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

	formatStructuredOutput(&b, insights)

	p.formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nReview comments have been posted directly on the GitHub PR.\n")
	b.WriteString("View the full Devin session: ")
	b.WriteString(sessionURL)
	b.WriteString("\n")

	return b.String()
}

// formatDevelopResult formats the development session result into a readable text summary.
func (p *Plugin) formatDevelopResult(sessionID, sessionURL string, status *devin.SessionStatus, messagesJSON string, msgErr error, insights *devin.SessionInsight) string {
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

	formatStructuredOutput(&b, insights)

	p.formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nView the full Devin session for details: ")
	b.WriteString(sessionURL)
	b.WriteString("\n")

	return b.String()
}

// formatSendMessageResult formats the result of a follow-up message into a readable text summary.
func (p *Plugin) formatSendMessageResult(sessionID string, status *devin.SessionStatus, messagesJSON string, msgErr error, insights *devin.SessionInsight) string {
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
	formatStructuredOutput(&b, insights)

	p.formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

	b.WriteString("\nThe session is still open. Send another message to continue, ")
	b.WriteString("or call complete_session to finalize it.\n")

	return b.String()
}

// formatCheckSessionResult formats a session status check into a readable text summary.
func (p *Plugin) formatCheckSessionResult(sessionID string, status *devin.SessionStatus, messagesJSON string, msgErr error, insights *devin.SessionInsight) string {
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
	formatStructuredOutput(&b, insights)

	p.formatDevinResponse(&b, sessionURL, messagesJSON, msgErr)

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
}
