# Spaces

## What it is

Spaces are Huginn's persistent conversation rooms. Instead of one-off sessions that lose context when you start a new one, a Space gives you an ongoing relationship with one or more agents. There are two types:

- **DM (Direct Message)** — a private, one-on-one conversation between you and a single agent. There is exactly one DM per agent; opening a DM with the same agent always returns the same space. DMs are immutable — they cannot be renamed, archived, or deleted.
- **Channel** — a named, multi-agent collaboration room. A Channel has a lead agent (the orchestrator) and up to 20 member agents who contribute specialized work. Channels have a name, an icon, and a color for visual identification.

Every Space persists its full conversation history across restarts. Messages you send into a Space become sessions within that Space, and the agent retains context from all prior sessions in the same Space.

### Key concepts

| Concept | Description |
|---------|-------------|
| **Lead agent** | The primary agent that orchestrates work in the Space. In a DM, this is the agent you are talking to. In a Channel, this agent receives a team roster in its system prompt and can delegate to member agents. |
| **Members** | Channel-only. A list of up to 20 agent names that participate in the channel. |
| **Icon & Color** | Channels can be customized with an icon and a hex color. DMs use the agent's default appearance. |
| **Unseen count** | A badge counter showing how many sessions in the Space have been updated since you last read it. |
| **Archived** | Channels can be soft-archived. Archived spaces are hidden from the default list but their data is preserved. DMs cannot be archived. |

---

## How to use it

### Direct Messages (DMs)

DMs are the simplest way to build an ongoing relationship with a specific agent.

**Opening a DM from the web UI:**

1. In the left sidebar, find the **Direct Messages** section.
2. Click the agent you want to message, or use the agent picker.
3. The DM space opens. If it already exists, you are returned to the existing space with all prior history intact.
4. Start typing. Every message is a new session within this DM space.

**When to use DMs:**

- You always go to the same agent for a specific kind of work (e.g., Mark handles code reviews, Steve handles architecture).
- You want the agent to remember every previous conversation you have had with it in one place.
- You want the simplest setup — no configuration required.

**From the TUI:**

```
/dm mark
```

Opens or resumes your DM with Mark.

### Channels

Channels are for project-scoped collaboration with multiple agents working as a team.

**Creating a Channel from the web UI:**

1. Click **New Channel** in the sidebar.
2. Give the channel a name (e.g., "Backend Refactor", "Q2 Launch").
3. Select a **lead agent** — this is the orchestrator who receives your messages first.
4. Add **member agents** — specialists the lead can delegate to.
5. Optionally pick an icon and color.
6. Click **Create**.

**Example — setting up a project channel:**

Channel: "API Redesign"
- Lead: Steve (architect)
- Members: Mark (code review), Chris (implementation)

When you send a message to this channel, Steve receives it. Steve's system prompt includes a team roster and can delegate subtasks to Mark and Chris, who work in threads within the same channel.

**From the TUI:**

```
/channel "API Redesign"
```

Opens or creates the named channel.

### Channel Memory Replication

When an agent in a Channel stores a memory (a decision, fact, constraint, or preference), that memory is automatically replicated to every other member agent's personal vault. This is **memory replication fan-out**.

How it works:

1. Mark, working in the "API Redesign" channel, records a decision: "Use cursor pagination for all list endpoints."
2. The memory replicator fans this out to Steve's and Chris's vaults.
3. Each replica is tagged with provenance: which agent created it, which channel it came from.
4. Anti-echo protection prevents replicated memories from being re-replicated.
5. Failed replications are retried automatically with exponential backoff (5s, 30s, 2m, 10m, up to 5 attempts).

Every agent on the team stays informed without any manual copy-paste.

### Workstreams

Workstreams are named projects that group sessions within a Space by topic or initiative.

**Creating a Workstream:**

1. Navigate to a Space in the web UI.
2. Open the Workstreams panel.
3. Click **New Workstream** and provide a name and optional description.
4. Tag sessions to the workstream as you work.

| Action | Description |
|--------|-------------|
| Create | Name and optional description |
| Tag session | Associate any session with the workstream (idempotent) |
| List sessions | See all sessions tagged to a workstream, newest first |
| Update | Change the name or description |
| Delete | Removes the workstream (sessions themselves are not deleted) |

---

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/spaces` | List spaces. Params: `kind` (dm/channel), `archived` (true), `cursor` (pagination token). |
| `GET` | `/api/v1/spaces/:id` | Get a single space by ID |
| `POST` | `/api/v1/spaces` | Create a channel. Body: `{name, lead_agent, member_agents, icon, color}` |
| `POST` | `/api/v1/spaces/dm/:agent` | Open or create a DM with the named agent (idempotent) |
| `PUT` | `/api/v1/spaces/:id` | Update a channel (name, icon, color, member_agents, lead_agent). Returns 403 for DMs. |
| `DELETE` | `/api/v1/spaces/:id` | Archive a channel. Returns 403 for DMs. |
| `POST` | `/api/v1/spaces/:id/read` | Mark the space as read (resets unseen count) |
| `GET` | `/api/v1/spaces/:id/sessions` | List sessions in the space |

**Create channel example:**

```bash
curl -X POST http://localhost:8421/api/v1/spaces \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Backend Refactor",
    "lead_agent": "steve",
    "member_agents": ["mark", "chris"],
    "icon": "🔧",
    "color": "#58a6ff"
  }'
```

**Pagination:** The space list uses opaque, base64-encoded cursor tokens. Each page returns a `next_cursor` value; pass it as `?cursor=<token>` to fetch the next page.

---

## Configuration

No special configuration is required. Spaces are managed from the web UI and stored in the same SQLite database as sessions (`~/.huginn/huginn.db`).

---

## Tips & common patterns

- **One DM per specialist** — Use DMs for ongoing relationships. Each agent remembers everything from all prior DM sessions.
- **Create Channels for projects, not topics** — A channel like "Q2 Launch" with architect + implementor + reviewer is more useful than a generic "Code Review" channel.
- **Keep member lists small** — 3–5 focused agents is the sweet spot. Large rosters increase memory fan-out volume.
- **Use Workstreams to organize within a Space** — When a channel covers multiple initiatives, tag sessions into workstreams.
- **Check unseen badges** — The sidebar shows which spaces have new activity. Click to clear the badge.
- **Archiving is reversible** — Archived channels are hidden but not deleted. Pass `?archived=true` to the list API to see them.

---

## Troubleshooting

**Cannot rename or archive a DM**
DMs are immutable by design. Create a Channel if you need a customizable space.

**Agent not found when creating a channel**
Check the agent name spelling in the Agents screen. Agent names are case-insensitive.

**Unseen count does not update**
Check the WebSocket connection status indicator. Refresh the page to reconnect.

**Memory replication failed**
Failed replications are queued and retried automatically (up to 5 times). Check that MuninnDB is running if you rely on cross-agent memory.

---

## See Also

- [Custom Agents](custom-agents.md) — defining the agents that participate in Spaces
- [Memory](memory.md) — how MuninnDB memory works across sessions
- [Sessions](sessions.md) — how individual sessions within a Space are stored
- [Multi-Agent](multi-agent.md) — delegation and orchestration patterns
- [Workflows](workflows.md) — scheduled automation (distinct from Space workstreams)
