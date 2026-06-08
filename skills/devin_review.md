# Devin Code Review Skill Guide

Use the Devin plugin to perform automated code reviews of pull requests. Devin posts inline comments directly on the GitHub PR.

## Workflow

### 1. Run a Code Review with `code_review`

Use `code_review` to have Devin review a pull request. Devin reviews every changed file, posts inline comments on the GitHub PR, and submits an overall review summary.

**Required parameter:**
- `pr_url` — full GitHub pull request URL (e.g. `https://github.com/org/repo/pull/123`)

**Optional parameter:**
- `instructions` — additional instructions or focus areas for the review

**What Devin reviews:**
- Code quality, readability, and maintainability
- Correctness and potential bugs
- Security concerns and vulnerabilities
- Adherence to best practices and coding conventions
- Improvement suggestions

**Example:**
```json
{
  "pr_url": "https://github.com/org/repo/pull/123",
  "instructions": "Pay close attention to SQL injection risks and input validation"
}
```

The response includes the session ID, status, pull request links, and Devin's review summary. Inline comments are posted directly on the GitHub PR.

By default the session is archived automatically after completion. If the plugin is configured with `archive_on_complete = "false"`, the session is left open and resumable instead — continue it with `send_message` and finalize it with `complete_session` (see below).

### 2. Retrieve Results with `check_session`

If the review response is missing Devin's summary, use `check_session` with the session ID to retrieve the full results including messages and session insights.

```json
{
  "session_id": "32fee96e7997499ca010301aa50eefce"
}
```

### 3. Follow Up with `send_message`

If you want Devin to re-review after changes or address a specific concern, use `send_message` to send a follow-up and wait for the response. The session must still be open, which requires `archive_on_complete = "false"` in the plugin settings.

**Required parameters:**
- `session_id` — the session to continue
- `message` — the follow-up instruction or question

```json
{
  "session_id": "32fee96e7997499ca010301aa50eefce",
  "message": "The author pushed a fix for the SQL injection comment — please re-review the updated query builder."
}
```

The plugin sends the message and polls until Devin finishes, then returns its updated summary (and Devin may post new inline comments on the PR). Call it as many times as needed.

### 4. Finalize with `complete_session`

When the review is complete and no further follow-ups are needed, call `complete_session` to archive the session and release its resources. After this the session can no longer be resumed.

```json
{
  "session_id": "32fee96e7997499ca010301aa50eefce"
}
```

This is the explicit counterpart to `archive_on_complete = "false"`.

### 5. Interpreting the Review Response

**Devin's Response section** — contains Devin's overall review summary describing what was found across the PR.

**Inline comments** — Devin posts detailed comments directly on the GitHub PR at specific lines. These are visible on the PR page in GitHub, not in the plugin response.

- If the response says "Devin returned an error in messaging", review the session directly at the provided URL
- If the response says "Devin did not return a message", the session completed but produced no message output — check the PR on GitHub for inline comments

**Session Insights** (via `check_session`) — provides additional analysis including issues found, action items, and a timeline of what Devin reviewed.

## Tips for Effective Code Reviews

- Use `instructions` to focus on specific concerns (e.g. "check for memory leaks" or "verify error propagation")
- Devin posts comments directly on GitHub, so reviewers see them alongside the diff
- Combine with `code_qa` to get both a code review and a QA test pass on the same PR
- For security-focused reviews, add instructions like "focus on authentication, authorization, and input sanitization"
