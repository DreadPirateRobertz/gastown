# gmail-mcp

Gas Town Gmail MCP server. Exposes three tools for Claude Code agents:

| Tool | Description |
|------|-------------|
| `gmail_send` | Send email via Gmail SMTP (nodemailer + app password) |
| `gmail_list_labels` | List all Gmail labels in the account |
| `gmail_organize_labels` | Create labels and apply them to messages |

**Account:** `halworker85@gmail.com`

---

## Setup

### 1. Create a Gmail App Password

App passwords require 2-Step Verification to be enabled on the account.

1. Go to [myaccount.google.com/security](https://myaccount.google.com/security)
2. Under "How you sign in to Google", open **2-Step Verification**
3. Scroll to the bottom → **App passwords**
4. Create a new app password (name: "Gas Town MCP")
5. Copy the 16-character code (spaces don't matter)

### 2. Build the server

```bash
cd plugins/gmail-mcp
npm install
npm run build
```

### 3. Register in Claude Code

Add to `~/.claude/settings.json` under `"mcpServers"`:

```json
{
  "mcpServers": {
    "gmail": {
      "command": "node",
      "args": ["/Users/hal/gt/gastown/plugins/gmail-mcp/dist/index.js"],
      "env": {
        "GMAIL_USER": "halworker85@gmail.com",
        "GMAIL_APP_PASSWORD": "your-16-char-app-password"
      }
    }
  }
}
```

**For crew agents** — the path should match the crew member's worktree. Use the
absolute path where `gastown` is cloned. The `dist/` build output is committed
so no local build step is required after pulling.

---

## Development

```bash
# Run without building
GMAIL_USER=halworker85@gmail.com GMAIL_APP_PASSWORD=xxxx npm run dev
```

---

## Tool reference

### `gmail_send`

```json
{
  "to": "someone@example.com",
  "subject": "Gas Town status update",
  "body": "Plain text content",
  "html": "<b>Optional HTML</b>",
  "cc": "cc@example.com",
  "bcc": "bcc@example.com"
}
```

### `gmail_list_labels`

No arguments. Returns JSON array of `{ name, delimiter, flags }` objects.

### `gmail_organize_labels`

```json
{
  "action": "create_and_apply",
  "label_name": "crew/gastown",
  "message_search": "FROM status@gastown.internal",
  "max_messages": 50
}
```

`action` values:
- `create` — create the label only
- `apply` — apply an existing label to matching messages
- `create_and_apply` — create then apply

`message_search` uses IMAP search syntax, e.g.:
- `"UNSEEN"` — unread messages
- `"FROM someone@example.com"` — by sender
- `"SUBJECT 'Gas Town'"` — by subject
- `"SINCE 1-Jan-2026"` — by date
