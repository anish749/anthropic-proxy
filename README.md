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

### Find and replace

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
