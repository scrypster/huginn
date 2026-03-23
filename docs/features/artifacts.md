# Artifacts

## What it is

Artifacts are structured, named outputs that agents produce during their work — separate from the chat message stream. When an agent generates something substantial that needs your review before it is applied (a code patch, a document, a data export, a multi-file implementation), it creates an Artifact rather than dumping raw content into the chat.

Each artifact has:
- **Title** — a human-readable name (e.g., "Add pagination to users API")
- **Kind** — the type of content
- **Status** — where it is in its review lifecycle
- **Content** — the payload, stored inline (up to 256 KB) or offloaded to disk (up to 10 MB)

Artifacts belong to a session and are tied to the agent that created them.

### Artifact kinds

| Kind | Description | Typical use |
|------|-------------|-------------|
| `code_patch` | A diff/patch file | Code changes too large for chat |
| `document` | Markdown or prose | Generated documentation, reports, READMEs |
| `timeline` | Timestamped event log | Audit trails, change histories |
| `structured_data` | JSON or CSV | Data exports, config files, analysis results |
| `file_bundle` | Tree of files with content | Multi-file implementations, project scaffolds |

---

## How to use it

### The artifact lifecycle

Every artifact moves through a review lifecycle:

```
draft → accepted | rejected | superseded | failed → deleted
```

| Status | Meaning |
|--------|---------|
| `draft` | Agent just created it; awaiting your review |
| `accepted` | You approved it; ready to download or apply |
| `rejected` | You rejected it; agent can revise |
| `superseded` | A newer version replaced it automatically |
| `failed` | Agent encountered an error producing it |
| `deleted` | Soft-deleted; hidden from all queries |

**Valid transitions:** draft → accepted, rejected, superseded, failed, deleted. Accepted/rejected/failed/superseded → deleted. Transitions are enforced server-side.

### Reviewing artifacts in the web UI

When an agent produces an artifact during a conversation:

1. An artifact card appears inline in the chat showing the title, kind, and a preview.
2. Click the card to expand it and see the full content.
3. Use the **Accept** or **Reject** buttons to set the status.
4. Optionally provide a rejection reason — the agent can see it and revise.
5. Click **Download** to save the artifact to your machine.

### What agents create artifacts for

- **Too large for chat** — A 500-line code patch is better reviewed as a downloadable diff.
- **Needs explicit approval** — Code changes and data exports should be reviewed before applying.
- **A deliverable** — Generated documentation and reports are standalone outputs.
- **Multi-file** — A `file_bundle` packages an entire directory tree.

### Downloading and applying

Artifacts can be downloaded as files. The filename is derived from the artifact title.

For `code_patch` artifacts, download and apply with:

```bash
# Download the diff
curl -o patch.diff http://localhost:8421/api/v1/artifacts/<id>/download

# Review
cat patch.diff

# Apply
git apply patch.diff
```

For `file_bundle` artifacts, the download contains the full directory tree as a tar or zip.

### Storage

| Detail | Value |
|--------|-------|
| Inline threshold | 256 KB — below this, content is stored in SQLite |
| File offload | Above 256 KB, content is written to `~/.huginn/artifacts/` |
| Maximum size | 10 MB per artifact |
| ID format | ULID |

---

## API Reference

### Session-scoped

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/sessions/:id/artifacts` | List artifact summaries. Params: `limit` (default 50, max 200), `after` (ULID cursor). |
| `GET` | `/api/v1/artifacts/:id` | Get a single artifact with full content |
| `PUT` | `/api/v1/artifacts/:id/status` | Update status. Body: `{status, reason}` |
| `DELETE` | `/api/v1/artifacts/:id` | Soft-delete (sets status to `deleted`) |
| `GET` | `/api/v1/artifacts/:id/download` | Download content as a file |

**List artifacts for a session:**

```bash
curl http://localhost:8421/api/v1/sessions/abc123/artifacts
```

Response (content omitted in list for performance):

```json
[
  {
    "id": "01HXYZ...",
    "kind": "code_patch",
    "title": "Add pagination to users API",
    "mime_type": "text/x-diff",
    "agent_name": "mark",
    "session_id": "abc123",
    "status": "draft",
    "created_at": "2026-03-21T14:30:00Z"
  }
]
```

**Accept an artifact:**

```bash
curl -X PUT http://localhost:8421/api/v1/artifacts/01HXYZ.../status \
  -H "Content-Type: application/json" \
  -d '{"status": "accepted"}'
```

**Reject with a reason:**

```bash
curl -X PUT http://localhost:8421/api/v1/artifacts/01HXYZ.../status \
  -H "Content-Type: application/json" \
  -d '{"status": "rejected", "reason": "Missing error handling for edge cases"}'
```

---

## Configuration

Artifacts require no special configuration and are enabled automatically.

| Setting | Default | Description |
|---------|---------|-------------|
| Artifacts directory | `~/.huginn/artifacts/` | Storage for large artifact content |
| Inline threshold | 256 KB | Above this, content is written to disk |
| Max artifact size | 10 MB | Hard limit per artifact |

---

## Tips & common patterns

- **Review before applying** — Artifacts exist so you can examine agent output before it touches your codebase.
- **Use rejection reasons** — When you reject an artifact, explain why. The agent reads the reason and can produce a better revision.
- **Check for superseded artifacts** — When an agent revises its work, the old artifact is automatically superseded. Always review the latest version (look for `draft` status, not `superseded`).
- **Apply patches with git** — Download `code_patch` artifacts and use `git apply` for a cleaner workflow than copy-pasting.
- **Paginate large lists** — Pass `?after=<last-artifact-id>` to paginate through large result sets.

---

## Troubleshooting

**"artifact content exceeds maximum size"**
Content is larger than 10 MB. Ask the agent to break the output into smaller artifacts.

**"invalid status transition"**
Check the current status first — for example, you cannot move a `rejected` artifact back to `accepted`.

**Artifact card not appearing in chat**
The web UI shows artifact cards automatically when an agent creates one. If none appear, check that you are using the web UI (`huginn tray`), not the TUI. Artifact cards are a web UI feature.

**Download returns empty**
The artifact's file-backed content may be missing from `~/.huginn/artifacts/`. If the file is gone, the content cannot be recovered.

---

## See Also

- [Sessions](sessions.md) — artifacts belong to sessions
- [Multi-Agent](multi-agent.md) — how delegated agents produce artifacts in threads
- [Spaces](spaces.md) — artifacts within Space-scoped sessions
- [Permissions](permissions.md) — write artifacts require approval gating
