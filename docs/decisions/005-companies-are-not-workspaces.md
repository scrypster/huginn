# ADR-005: Companies are not workspaces

**Date:** 2026-08-26
**Status:** Accepted

## Context

Huginn already has two "workspace" meanings: HuginnCloud satellite
switching (feels like Slack workspace switch, one login, many machines)
and `huginn.workspace.json` (project root + default vault). Local chat
already has spaces (channels and DMs) with a lead and a roster.

The missing product is company isolation: agents in one business must
not know the people, tools, or memory of another. Reusing "workspace"
for that would collide with Cloud and with the Slack muscle memory
Cloud already spent.

## Decision

Three layers, three words:

1. **Machine** — HuginnCloud satellite. Compute. Not a company.
2. **Company** — isolation boundary. Roster, connections, and vault.
   Agents only see teammates seated in the same company.
3. **Channel / DM** — conversation. Already exists as `spaces.Space`.
   A space belongs to one company.

Do not name companies "workspace", "org", or "team" in the UI. Space
already has an unused `team_id` field; that becomes `company_id`.

**Desk vs company.** Think holding company: SpaceX, Grok, Boring,
Neuralink each have their own bots. Those bots never see the other
shops. A few people sit above (you, a chief of staff, maybe a
generalist) and can be seated in zero, one, or many companies.

Winston is one person. He sits at the desk by default. In a company
channel he only sees that company's roster, vault, and connections.
At the desk he can see company names and route work into them. He
does not pour one company's memory into another. Specialists never
cross companies unless added. The same agent name may sit in more
than one company member list.

## Consequences

- Cloud switcher and company switcher can sit in the chrome without
  looking like two Slack workspace pickers.
- First code cut: persist Company (id, name, vault, members) and
  attach `company_id` on Space. No Cloud/relay changes.
- Per-agent grants stay the guardrail inside a company. Company
  membership is the guardrail between companies.
- Huginn still works if MuninnDB is down; a missing company vault
  degrades to no memory, not a silent substitute.
