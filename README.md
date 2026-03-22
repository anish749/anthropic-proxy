# anthropic-proxy

A local HTTP proxy that sits between Claude Code and the Anthropic API. It rewrites the system prompt and tool descriptions before forwarding — letting you swap in custom personalities, tone, style instructions, or patch tool behavior.

## Install

```bash
curl -sL https://raw.githubusercontent.com/anish749/anthropic-proxy/main/install.sh | bash
```

This downloads the latest release and installs to `~/.local/bin/anthropic-proxy`. Override the install directory with `INSTALL_DIR`:

```bash
curl -sL https://raw.githubusercontent.com/anish749/anthropic-proxy/main/install.sh | INSTALL_DIR=/usr/local/bin bash
```

Or build from source:

```bash
git clone https://github.com/anish749/anthropic-proxy.git
cd anthropic-proxy
go build -ldflags="-s -w" -o anthropic-proxy .
```

## Usage

```sh
anthropic-proxy --port 8080
```

Then start Claude Code pointing at the proxy:

```sh
ANTHROPIC_BASE_URL=http://localhost:8080 claude
```

### Commands

| Command | Description |
|---------|-------------|
| *(default)* | Start the proxy server |
| `login` | Log in via Anthropic OAuth |

Run `anthropic-proxy --help` or `anthropic-proxy <command> --help` for details.

### Proxy flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | Port to listen on |
| `--log` | `false` | Log every request to the `requests/` directory |
| `--log-format` | `json` | Output format for logged requests (`json`, `yaml`) |
| `--swap-creds` | `false` | Replace client credentials with logged-in OAuth token |
## Rewriting rules

All `*.yaml` files in `prompts/` are loaded and merged at startup. Every rule uses the same schema with a `type` field to distinguish between system prompt and tool description replacements. You can organize rules across as many files as you like — split by concern, keep everything in one file, whatever works for you.

Files matching `*replacements.local.yaml` are gitignored for local overrides.

### Rule schema

```yaml
- type: system          # or "tool"
  block: 2              # system: which prompt block to target
  tool: Bash            # tool: which tool name to target
  find: "text to find"
  replace: "replacement"
  regex: false          # tool: treat find as a regex (default: false)
  disabled: false       # skip this rule (default: false)
  warn_after: 5         # warn if unmatched after N evals (default: never)
```

### System prompt rules (`type: system`)

Find-and-replace within a system prompt block by index:

```yaml
- type: system
  block: 2
  find: "Your responses should be short and concise."
  replace: "You are a friendly pirate. Respond in pirate speak."
```

Full replacement files (`prompts/system-{i}-replace.txt`) are also supported and take precedence over find-and-replace rules for the same block.

### Tool description rules (`type: tool`)

Patch tool descriptions before they reach the model:

```yaml
- type: tool
  tool: Bash
  find: "# Creating pull requests\n..."
  replace: "# Creating pull requests\n..."

- type: tool
  tool: Bash
  regex: true
  warn_after: 5
  find: "Co-Authored-By: Claude .+ <noreply@anthropic\\.com>"
  replace: ""
```

Multiple rules can target the same tool and are applied in order.

### Block indices

The Claude Code system prompt is an array of text blocks. Typically:

| Index | Content |
|-------|---------|
| 0 | Billing metadata |
| 1 | Identity intro |
| 2 | Main instructions, tone, and style |
| 3 | Memory and environment |

Blocks 0 and 1 are generally left unchanged. Blocks 2 and 3 are where personality and style live.

### Diagnostics

Unmatched find strings log a warning correlated with the upstream `Request-Id`. Tool rules track match stats and log a summary every 50 requests. Invalid YAML is a fatal startup error.

## Request logging

Opt-in with the `--log` flag:

```sh
anthropic-proxy --port 8080 --log
```

Each API call creates files in `requests/` named `{timestamp}-{request-id}-{model}-{part}.json`:

- `tools.json` — tool definitions
- `messages.json` — conversation messages
- `system.json` — system prompt
- `system-reminders.json` — `<system-reminder>` blocks extracted from user messages (if any)
- `usage.json` — token usage from the response

Without `--log`, requests are still logged automatically when rewrite rules produce warnings, for debugging.

## OAuth login

To use the proxy with credential swapping (replacing your client's API key with an OAuth token):

```sh
anthropic-proxy login
anthropic-proxy --swap-creds
```

