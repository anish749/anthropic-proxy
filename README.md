# anthropic-proxy

A local HTTP proxy that sits between Claude Code and the Anthropic API. It rewrites the system prompt before forwarding — letting you swap in custom personalities, tone, or style instructions.

## Usage

```sh
go run . -port 8080
```

Then start Claude Code pointing at the proxy:

```sh
ANTHROPIC_BASE_URL=http://localhost:8080 claude
```

## System prompt rewriting

Create a `prompts/` directory to modify the system prompt before it reaches Anthropic.

### Full replacement

Replace an entire system prompt block by index:

```
prompts/system-2-replace.txt
```

The file contents become the new text for that block.

### Find and replace in system prompt blocks

Create `prompts/replacements.yaml`:

```yaml
- block: 2
  find: |
    # Tone and style
     - Your responses should be short and concise.
  replace: |
    # Custom personality
    You are a friendly pirate. Respond in pirate speak.

- block: 3
  find: "some text"
  replace: "new text"
  disabled: true  # skip this rule
```

Full replacement files take precedence over find-and-replace rules for the same block. Unmatched find rules log a warning.

### Block indices

The Claude Code system prompt is an array of text blocks. Typically:

| Index | Content |
|-------|---------|
| 0 | Billing metadata |
| 1 | Identity intro |
| 2 | Main instructions, tone, and style |
| 3 | Memory and environment |

Blocks 0 and 1 are generally left unchanged. Blocks 2 and 3 are where personality and style live.

### Find and replace in tool descriptions

Create `prompts/tool_replacements.yaml` to patch tool descriptions before they reach the model. Each rule targets a tool by name and performs a substring find-and-replace on its description:

```yaml
- tool: Bash
  find: "# Creating pull requests\n..."
  replace: "# Creating pull requests\n..."

- tool: Read
  find: "some text to remove"
  replace: ""
  disabled: true  # skip this rule
```

Fields:

| Field | Required | Description |
|-------|----------|-------------|
| `tool` | yes | Tool name to target (e.g. `Bash`, `Read`, `Edit`, `Grep`, `Glob`, `Write`) |
| `find` | yes | Substring to find in the tool's description |
| `replace` | yes | Replacement text (`""` to delete) |
| `disabled` | no | Set to `true` to skip the rule |

Multiple rules can target the same tool and are applied in order. Unmatched find strings log a warning. Invalid YAML in either replacements file is a fatal startup error.

## Request logging

Opt-in with the `-log` flag:

```sh
go run . -port 8080 -log
```

Each API call creates files in `requests/` named `{timestamp}-{request-id}-{model}-{part}.json`:

- `tools.json` — tool definitions
- `messages.json` — conversation messages
- `system.json` — system prompt
- `usage.json` — token usage from the response
