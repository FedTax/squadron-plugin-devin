# Devin Plugin Skill Guide

Use the Devin plugin to delegate code development tasks to Devin AI and retrieve results.

## Workflow

### 1. Develop Code with `code_develop`

Use `code_develop` to assign a development task to Devin. Devin clones the repo, implements changes, runs tests, and opens a pull request.

**Required parameters:**
- `repo_url` — full GitHub repository URL (e.g. `https://github.com/org/repo`)
- `task` — clear description of what to implement

**Optional parameters:**
- `branch` — branch name for Devin to create (Devin picks one if omitted)
- `instructions` — additional context, constraints, or coding guidelines
- `title` — session title, e.g. `"DEV-8126 investigate"` (Devin generates one if omitted)
- `tags` — session tags, e.g. `["ratevariant", "investigate"]`, so the sessions a mission spawned can be found later
- `prompt_mode` — `"default"` or `"raw"`

**`prompt_mode`:** the default wraps your task in a fixed workflow — create a branch, implement,
add tests, commit, open a PR. That is wrong for two common jobs: a **read-only investigation**
(which must not branch or open a PR) and a **follow-on stage** that has to push to a branch and
PR that already exist. For those, pass `"prompt_mode": "raw"`; your `task` and `instructions`
then reach Devin verbatim, so they must carry every instruction the job needs, including what
not to do.

**Tips for effective prompts:**
- Be specific about what to change and where in the codebase
- Mention coding conventions, test requirements, or files to modify
- Reference existing patterns in the repo when relevant
- Include acceptance criteria so Devin knows when the task is complete

**Example:**
```json
{
  "repo_url": "https://github.com/org/repo",
  "task": "Add pagination to the GET /users API endpoint using cursor-based pagination",
  "branch": "feature/users-pagination",
  "instructions": "Follow the existing pagination pattern used in the /orders endpoint. Add tests."
}
```

The response includes the session ID, status, any pull request links, the session's structured
output (if its playbook defines a schema), and Devin's final message. The full transcript is not
returned — follow the session URL for it, or set `raw_messages = "true"` in the plugin settings
if an agent genuinely needs the whole conversation.

By default the session is archived automatically after completion. If the plugin is configured with `archive_on_complete = "false"`, the session is left open and resumable instead — continue it with `send_message` and finalize it with `complete_session` (see below).

### 2. Check a Session with `check_session`

Use `check_session` to inspect a Devin session after it completes. This returns the full status, Devin's messages, pull request links, and session insights (action items, issues, timeline).

**Required parameter:**
- `session_id` — the Devin session ID returned by `code_develop`, `code_qa`, or `code_review`

**Example:**
```json
{
  "session_id": "32fee96e7997499ca010301aa50eefce"
}
```

Use `check_session` when:
- The `code_develop` response is missing Devin's message and you need to retrieve it
- You want to review session insights (issues found, action items, timeline)
- You need to check on a session that was created earlier

### 3. Continue a Session with `send_message`

Use `send_message` to send a follow-up message to an open session and wait for Devin to finish responding. Use this to answer a question Devin asked, give additional instructions, or request changes after reviewing its work.

The session must still be open (not archived). This requires `archive_on_complete = "false"` in the plugin settings; otherwise `code_develop` archives the session as soon as it finishes.

**Required parameters:**
- `session_id` — the Devin session ID to continue
- `message` — the follow-up message (instruction, answer, or change request)

**Example:**
```json
{
  "session_id": "32fee96e7997499ca010301aa50eefce",
  "message": "The PR looks good, but please also add a test for the empty-cursor case."
}
```

The plugin sends the message and polls until Devin finishes the follow-up work, then returns Devin's updated response. You can call `send_message` multiple times to keep iterating.

### 4. Finalize a Session with `complete_session`

Use `complete_session` upon mission finalization, once no further follow-up messages are needed. It archives the session and releases its resources. After completion the session can no longer be resumed with `send_message`.

**Required parameter:**
- `session_id` — the Devin session ID to finalize

**Example:**
```json
{
  "session_id": "32fee96e7997499ca010301aa50eefce"
}
```

This is the explicit counterpart to `archive_on_complete = "false"`: when sessions are left resumable, call `complete_session` to archive them when the work is truly done.

### 5. Interpreting Responses

**Devin's Response section** — contains Devin's own summary of what it did. Use this to understand the changes and decide next steps.

- If the response says "Devin returned an error in messaging", review the session directly at the provided URL
- If the response says "Devin did not return a message", the session completed but produced no message output — continue to the next task

**Session Insights section** (check_session only) — contains AI-generated analysis:
- **Issues** — problems encountered during the session
- **Action Items** — follow-up tasks or improvements
- **Timeline** — key milestones and what Devin did at each stage

**Pull Requests** — if Devin opened a PR, the URL and state are included in the response. Use this to review or merge the changes.

## Other Tools

### `code_qa`
Performs a QA review of a pull request. Devin checks out the branch, runs tests, and reports bugs, coverage gaps, and regressions.

```json
{
  "pr_url": "https://github.com/org/repo/pull/123",
  "instructions": "Focus on error handling and edge cases"
}
```

### `code_review`
Performs a code review of a pull request. Devin reviews the diff and posts inline comments directly on the GitHub PR.

```json
{
  "pr_url": "https://github.com/org/repo/pull/123",
  "instructions": "Check for security vulnerabilities"
}
```

Both tools return a session ID that can be passed to `check_session` if you need to retrieve Devin's full response later.
