# squadron-plugin-devin

A [Squadron](https://github.com/mlund01/squadron-sdk) plugin that integrates [Devin AI](https://devin.ai) for automated pull request QA, code review, and code development.

This plugin uses the [Devin v3 API](https://docs.devin.ai/api-reference/overview).

## Tools

### `code_qa`

Performs a full QA review of a pull request. Devin checks out the PR branch, analyzes changes, runs existing tests, and returns a comprehensive summary.

The QA review covers:
- Bug detection, edge cases, and logic errors
- Error handling adequacy
- Test execution and failure reporting
- Missing test coverage for new/changed code
- Regression risks in related functionality
- Alignment with PR description and linked issues
- Performance concerns

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `pr_url` | string | yes | Full URL of the GitHub PR (e.g. `https://github.com/org/repo/pull/123`) |
| `instructions` | string | no | Additional instructions or focus areas for the QA review |
| `title` | string | no | Title for the Devin session. Devin generates one if omitted. |
| `tags` | string[] | no | Tags to apply to the Devin session, for filtering sessions later |

### `code_review`

Performs a full code review of a pull request. Devin reviews the diff, posts inline comments directly on the GitHub PR, and submits an overall review summary.

The code review covers:
- Every changed file in the PR diff
- Code quality, readability, and maintainability
- Correctness and potential bugs
- Security concerns and vulnerabilities
- Adherence to best practices and coding conventions
- Improvement suggestions

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `pr_url` | string | yes | Full URL of the GitHub PR (e.g. `https://github.com/org/repo/pull/123`) |
| `instructions` | string | no | Additional instructions or focus areas for the code review |
| `title` | string | no | Title for the Devin session. Devin generates one if omitted. |
| `tags` | string[] | no | Tags to apply to the Devin session, for filtering sessions later |

### `code_develop`

Develops code on a repository. Devin clones the repo, implements the requested changes, runs tests, and opens a pull request with the completed work.

Use this for:
- Feature development
- Bug fixes
- Refactoring
- Any code changes on a repository

Devin will follow existing code conventions, add or update tests, and open a PR with a detailed description. The repository is passed to Devin via the v3 `repos` field, so Devin has direct access to it.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `repo_url` | string | yes | Full URL of the GitHub repository (e.g. `https://github.com/org/repo`) |
| `task` | string | yes | Description of the development task to perform |
| `branch` | string | no | Branch name for Devin to create. If omitted, Devin chooses an appropriate name. |
| `instructions` | string | no | Additional context, constraints, or coding guidelines |
| `title` | string | no | Title for the Devin session (e.g. `DEV-8126 investigate`). Devin generates one if omitted. |
| `tags` | string[] | no | Tags to apply to the Devin session (e.g. `["ratevariant", "investigate"]`), for filtering sessions later |
| `prompt_mode` | string | no | `default` (default) wraps the task in the branch / tests / commit / PR workflow above. `raw` sends `task` and `instructions` verbatim with no added steps. |

Use `prompt_mode = "raw"` when the default workflow is wrong for the job: a read-only
investigation that must not create a branch or PR, or a later stage that must push to a branch
and PR that already exist. In `raw` mode the task text is the whole prompt, so it has to carry
its own instructions.

### `check_session`

Checks the status of an existing Devin session. Returns the full session status including current state, pull requests, and Devin's messages. Use this to inspect a session that was previously created by another tool or to check on a long-running session.

The session ID is returned by `code_qa`, `code_review`, and `code_develop` when they create a session.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `session_id` | string | yes | The Devin session ID (e.g. `32fee96e7997499ca010301aa50eefce`) |

### `send_message`

Sends a follow-up message to an existing Devin session and waits for Devin to finish responding. Use this to continue a conversation with a session that is waiting for user input, for example to answer a question, give additional instructions, or request changes.

The session must still be open (not archived). To keep sessions resumable after `code_qa`, `code_review`, or `code_develop` complete, set `archive_on_complete = "false"` in the plugin settings.

After sending the message, the plugin reuses the same polling logic as the other tools, waiting until Devin finishes the follow-up work before returning its response.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `session_id` | string | yes | The Devin session ID to send the message to |
| `message` | string | yes | The message to send to Devin (follow-up instruction, answer, or change request) |

### `complete_session`

Finalizes and archives a Devin session. Call this upon mission finalization once no further follow-up messages are needed, to archive the session and release its resources. After completion the session can no longer be resumed with `send_message`.

This is the explicit counterpart to `archive_on_complete = "false"`: when sessions are left resumable, use `complete_session` to archive them when the work is truly done.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `session_id` | string | yes | The Devin session ID to finalize and archive |

## Prerequisites

- Go 1.23+
- A [Devin AI](https://devin.ai) account with v3 API access
- A Devin **service user** with an API key (starts with `cog_`)
- Your Devin **organization ID**
- Devin must already have access to the GitHub repos you want to review or develop on

## Devin v3 API Setup

The v3 API uses service user tokens instead of personal API keys. Follow these steps to get set up:

1. **Create a service user** in your Devin organization:
   - Go to your Devin dashboard at [app.devin.ai](https://app.devin.ai)
   - Navigate to **Settings > Service Users**
   - Create a new service user and assign it a role with the `UseDevinSessions` permission

2. **Copy the API key** — it starts with `cog_` and is only shown once. Store it securely.

3. **Find your organization ID** — visible on the **Settings > Service Users** page in the Devin dashboard.

4. **Grant repo access** — ensure Devin has access to the GitHub repositories you want to work with. This is configured in your Devin organization's GitHub integration settings.

For more details, see the [Devin API documentation](https://docs.devin.ai/api-reference/overview).

## Build

```bash
# Clone the plugin project
git clone git@github.com:ericlakich/squadron-plugin-devin.git

# Build
cd squadron-plugin-devin
go mod tidy
go build -o plugin .

# Install into Squadron's plugin directory
mkdir -p ~/.squadron/plugins/devin/local
cp plugin ~/.squadron/plugins/devin/local/plugin
```

## Configuration

Add the plugin to your Squadron HCL config:

```hcl
plugin "devin" {
  version = "local"
  settings = {
    api_key              = "<your-devin-service-user-key>"
    org_id               = "<your-devin-org-id>"
    poll_timeout_minutes = "60"
    archive_on_complete  = "true"
  }
}
```

Then attach the tools to an agent:

```hcl
agent "reviewer" {
  model = models.anthropic.claude_sonnet_4
  tools = [plugins.devin.code_qa, plugins.devin.code_review, plugins.devin.code_develop, plugins.devin.check_session, plugins.devin.send_message, plugins.devin.complete_session]
}
```

### Settings

| Setting | Required | Description |
|---------|----------|-------------|
| `api_key` | yes | Devin service user API key (starts with `cog_`). Created under **Settings > Service Users** in the Devin dashboard. |
| `org_id` | yes | Devin organization ID. Found on the **Settings > Service Users** page in the Devin dashboard. |
| `poll_timeout_minutes` | no | Maximum time in minutes to wait for a Devin session to complete. Defaults to `60`. Increase for long-running development tasks. |
| `archive_on_complete` | no | Whether `code_qa`, `code_review`, and `code_develop` archive their session once Devin finishes. Defaults to `true`. Set to `false` to leave sessions resumable so they can be continued with `send_message` and finalized with `complete_session`. |
| `raw_messages` | no | Whether tool results carry the session's entire messages JSON payload. Defaults to `false`: results carry Devin's final message, the session's structured output, and PR links. Set to `true` for the full transcript, which is large enough to crowd out the rest of the caller's context. |

## How It Works

1. The agent invokes a tool (`code_qa`, `code_review`, or `code_develop`) with the required parameters.
2. The plugin creates a new Devin session via the [Devin v3 API](https://docs.devin.ai/api-reference/overview) (`POST /v3/organizations/{org_id}/sessions`).
3. The plugin polls the session status every 15 seconds (up to `poll_timeout_minutes`, default 60) until Devin finishes.
4. The session is archived (unless `archive_on_complete = "false"`) and the result — including Devin's messages — is returned as a text summary.

When `archive_on_complete` is `false`, sessions are left open after they finish. Use `send_message` to continue working with a session (the plugin sends the message and polls until Devin finishes again), and `complete_session` to archive it once the mission is finalized.

For `code_review`, Devin also posts inline review comments directly on the GitHub PR during its session.

For `code_develop`, the repository URL is passed via the v3 `repos` field so Devin has direct access. Devin implements the changes and opens a pull request. The PR link is included in the result.

For `check_session`, the plugin fetches the current status and message history for an existing session without creating a new one. This is useful for inspecting sessions created by other tools or checking on long-running work.

All tools respect context cancellation, so Squadron can terminate long-running sessions cleanly. Transient API errors during polling are retried automatically (up to 5 consecutive failures).

## Project Structure

```
squadron-plugin-devin/
  main.go          # Entry point - registers the plugin with Squadron
  plugin.go        # ToolProvider implementation, tool definitions, prompt builders
  devin/
    client.go      # HTTP client for the Devin AI v3 API
  go.mod
  go.sum
```

## License

See [LICENSE](LICENSE) for details.
