/**
 * gmail-mcp — Gas Town Gmail MCP server
 *
 * Tools:
 *   gmail_send           — Send email via Gmail SMTP (nodemailer + app password)
 *   gmail_list_labels    — List all Gmail labels via IMAP
 *   gmail_organize_labels — Create labels and apply them to messages
 *
 * Config (environment variables):
 *   GMAIL_USER         Gmail address (default: halworker85@gmail.com)
 *   GMAIL_APP_PASSWORD Gmail App Password (16-char, spaces optional)
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import nodemailer from "nodemailer";
import { ImapFlow } from "imapflow";
import { z } from "zod";

// ─── Config ──────────────────────────────────────────────────────────────────

const GMAIL_USER = process.env.GMAIL_USER ?? "halworker85@gmail.com";
const GMAIL_APP_PASSWORD = (process.env.GMAIL_APP_PASSWORD ?? "").replace(
  /\s/g,
  ""
);

if (!GMAIL_APP_PASSWORD) {
  process.stderr.write(
    "ERROR: GMAIL_APP_PASSWORD env var is required.\n" +
      "Generate one at: https://myaccount.google.com/apppasswords\n"
  );
  process.exit(1);
}

// ─── SMTP transport (send only) ───────────────────────────────────────────────

function makeTransport() {
  return nodemailer.createTransport({
    host: "smtp.gmail.com",
    port: 465,
    secure: true,
    auth: { user: GMAIL_USER, pass: GMAIL_APP_PASSWORD },
  });
}

// ─── IMAP client factory ──────────────────────────────────────────────────────

function makeImapClient() {
  return new ImapFlow({
    host: "imap.gmail.com",
    port: 993,
    secure: true,
    auth: { user: GMAIL_USER, pass: GMAIL_APP_PASSWORD },
    logger: false,
  });
}

// ─── Tool implementations ─────────────────────────────────────────────────────

async function gmailSend(args: {
  to: string;
  subject: string;
  body: string;
  html?: string;
  cc?: string;
  bcc?: string;
}): Promise<string> {
  const transport = makeTransport();
  const info = await transport.sendMail({
    from: GMAIL_USER,
    to: args.to,
    subject: args.subject,
    text: args.body,
    ...(args.html ? { html: args.html } : {}),
    ...(args.cc ? { cc: args.cc } : {}),
    ...(args.bcc ? { bcc: args.bcc } : {}),
  });
  const accepted = (info.accepted as string[])?.join(", ") ?? args.to;
  return `Sent. Message-ID: ${info.messageId}, accepted: ${accepted}`;
}

async function gmailListLabels(): Promise<string> {
  const client = makeImapClient();
  await client.connect();
  try {
    const mailboxes = await client.list();
    const labels = mailboxes.map((m) => ({
      name: m.path,
      delimiter: m.delimiter,
      flags: Array.from(m.flags ?? []),
    }));
    return JSON.stringify(labels, null, 2);
  } finally {
    await client.logout();
  }
}

async function gmailOrganizeLabels(args: {
  action: "create" | "apply" | "create_and_apply";
  label_name: string;
  message_search?: string;
  max_messages?: number;
}): Promise<string> {
  const client = makeImapClient();
  await client.connect();
  const results: string[] = [];

  try {
    if (args.action === "create" || args.action === "create_and_apply") {
      const existing = await client.list();
      const exists = existing.some((m) => m.path === args.label_name);
      if (exists) {
        results.push(`Label already exists: ${args.label_name}`);
      } else {
        await client.mailboxCreate(args.label_name);
        results.push(`Created label: ${args.label_name}`);
      }
    }

    if (args.action === "apply" || args.action === "create_and_apply") {
      if (!args.message_search) {
        results.push("ERROR: message_search is required for apply action");
        return results.join("\n");
      }

      await client.mailboxOpen("INBOX");
      const limit = args.max_messages ?? 20;
      const uids: number[] = [];

      for await (const msg of client.fetch(args.message_search, {
        uid: true,
      })) {
        uids.push(msg.uid);
        if (uids.length >= limit) break;
      }

      if (uids.length === 0) {
        results.push(`No messages matched: ${args.message_search}`);
      } else {
        // In Gmail, copying to a label mailbox applies the label
        await client.messageCopy(uids.join(","), args.label_name, {
          uid: true,
        });
        results.push(
          `Applied label '${args.label_name}' to ${uids.length} message(s)`
        );
      }
    }
  } finally {
    await client.logout();
  }

  return results.join("\n");
}

// ─── MCP server ───────────────────────────────────────────────────────────────

const server = new McpServer({
  name: "gmail-mcp",
  version: "1.0.0",
});

server.registerTool(
  "gmail_send",
  {
    description:
      "Send an email from the configured Gmail account via SMTP. Use for outbound notifications, status reports, and crew communications.",
    inputSchema: {
      to: z.string().describe("Recipient email address(es), comma-separated"),
      subject: z.string().describe("Email subject line"),
      body: z.string().describe("Plain-text email body"),
      html: z
        .string()
        .optional()
        .describe("Optional HTML body (HTML clients use this instead of body)"),
      cc: z.string().optional().describe("CC recipients, comma-separated"),
      bcc: z.string().optional().describe("BCC recipients, comma-separated"),
    },
  },
  async ({ to, subject, body, html, cc, bcc }) => {
    const text = await gmailSend({ to, subject, body, html, cc, bcc });
    return { content: [{ type: "text" as const, text }] };
  }
);

server.registerTool(
  "gmail_list_labels",
  {
    description:
      "List all Gmail labels (mailboxes) in the configured account. Returns JSON array of { name, delimiter, flags }.",
    inputSchema: {},
  },
  async () => {
    const text = await gmailListLabels();
    return { content: [{ type: "text" as const, text }] };
  }
);

server.registerTool(
  "gmail_organize_labels",
  {
    description:
      "Create Gmail labels and/or apply them to messages. Build a label taxonomy for crew reports (e.g. 'crew/gastown', 'reports/convoy').",
    inputSchema: {
      action: z
        .enum(["create", "apply", "create_and_apply"])
        .describe(
          "create: make a new label; apply: tag messages with existing label; create_and_apply: both"
        ),
      label_name: z
        .string()
        .describe("Label name (e.g. 'crew/gastown', 'reports/convoy')"),
      message_search: z
        .string()
        .optional()
        .describe(
          "IMAP search string for messages to label (required for apply/create_and_apply). E.g. 'FROM someone@example.com', 'UNSEEN', 'SUBJECT Gas Town'."
        ),
      max_messages: z
        .number()
        .int()
        .min(1)
        .max(100)
        .optional()
        .describe("Max messages to label (default 20, max 100)"),
    },
  },
  async ({ action, label_name, message_search, max_messages }) => {
    const text = await gmailOrganizeLabels({
      action,
      label_name,
      message_search,
      max_messages,
    });
    return { content: [{ type: "text" as const, text }] };
  }
);

// ─── Start ────────────────────────────────────────────────────────────────────

const transport = new StdioServerTransport();
await server.connect(transport);
