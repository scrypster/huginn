# Agent Toolbelt

## What it is

The toolbelt is a per-agent list of connections. When you assign a toolbelt to
an agent, that agent can only see and use the providers you listed — nothing
else. An agent with `aws-prod` and `github-myorg` in its toolbelt knows those
two connections exist. It has no idea your Google, Slack, or any other
connection is configured.

---

## Why it matters

Different agents do different jobs, and different jobs carry different risks.

Your DevOps agent needs to describe EC2 instances, create GitHub releases, and
post to Slack. It should touch AWS production. Your coding assistant, on the
other hand, should never know AWS production exists. If a prompt injection
attack tricks your coding assistant into trying to delete an S3 bucket, it
simply cannot — the AWS tools are not in its toolbelt, so the model never even
learned they exist.

The toolbelt gives you production isolation at the agent level:

- A **DevOps agent** gets `aws-prod` (with approval gate on) and
  `github-myorg`.
- A **coding assistant** gets `github-myorg` only.
- A **data analyst** gets `google-workspace` and `slack-company`.

Each agent can do exactly its job. None can do the others'.

---

## How to configure in the web UI

1. Open the Huginn web UI and go to **Agents**.
2. Select the agent you want to configure and click **Edit**.
3. Scroll to the **Connections** section.
4. Click **Add connection** and select a provider from the dropdown. The
   dropdown shows only connections you have already set up in Huginn.
5. Toggle **Approval gate** on or off for each connection.
   - Off: the agent uses this connection freely, including in headless and
     autonomous mode.
   - On: the agent can read from this connection freely, but any write or
     action always prompts you first — even if you started Huginn with
     automated mode enabled.
6. Save the agent. The toolbelt takes effect on the next session for that
   agent.

---

## How to configure in the agent JSON file

Agent definitions live at `~/.huginn/agents/<name>.json`. The `toolbelt` field
is an array of connection entries.

```json
{
  "name": "DevOps",
  "toolbelt": [
    {
      "connection_id": "aws-prod",
      "provider": "aws",
      "approval_gate": true
    },
    {
      "connection_id": "github-myorg",
      "provider": "github",
      "approval_gate": false
    },
    {
      "connection_id": "slack-company",
      "provider": "slack",
      "approval_gate": false
    }
  ]
}
```

Fields:

| Field | Required | Description |
|---|---|---|
| `connection_id` | Yes | The name of the connection as configured in Huginn |
| `provider` | Yes | The provider type: `aws`, `github`, `google`, `slack`, etc. |
| `profile` | No | For providers with named profiles (e.g., AWS), pins to a specific profile |
| `approval_gate` | No | `true` to always prompt before writes; defaults to `false` |

---

## The approval gate explained

The approval gate is a safety checkpoint for connections to critical systems.

When approval gate is **off**, the agent uses the connection freely. In a
headless routine or a scheduled task, the agent can read and write to that
provider without interrupting you. This is fine for low-risk connections like
a staging environment or a read-only data source.

When approval gate is **on**, reads are always allowed without prompting. But
any time the agent is about to make a change — creating a resource, modifying
a file, posting a message, running a command — Huginn pauses and shows you a
prompt:

```
DevOps agent wants to: aws_ec2_terminate_instance
Instance: i-0abc123def456
[Allow once]  [Allow all for this session]  [Deny]
```

You can allow it once (just this call), allow all writes to this provider for
the rest of the session, or deny it entirely.

**The approval gate cannot be bypassed by automation flags.** If you start
Huginn with `--dangerously-skip-permissions` to run a fully autonomous
pipeline, the approval gate still fires for any connection where you set
`approval_gate: true`. This is intentional — the gate is a per-agent policy
you set when configuring the agent, not a per-run decision. A CI pipeline
cannot silently override it.

---

## Best practices

**Give each agent only the connections it actually needs.** A data analyst
does not need GitHub. A documentation agent does not need AWS. Start with the
minimum and add connections only when a task actually requires them.

**Enable the approval gate on every production connection.** Any agent that
can reach a production AWS account, your primary GitHub organization, or your
company Slack should have `approval_gate: true`. Reads are free; you are only
adding a checkpoint for writes.

**An agent with no toolbelt can use ALL connections.** If you leave the
toolbelt empty, the agent has access to every connection configured on your
machine. This is the default for backward compatibility, but it should be an
explicit choice, not an accident. Review agents with empty toolbelts regularly.

**Use separate connection IDs for prod and non-prod.** Set up `aws-prod` and
`aws-staging` as distinct connections. Assign only `aws-staging` to agents
that do not need production access. This makes toolbelt entries unambiguous.

---

## Provider reference

| Provider | Connection type | Notes |
|---|---|---|
| `aws` | CLI (`aws`) | Reads named profiles; `profile` field selects a specific one |
| `github` | CLI (`gh`) | Reads `gh` auth state; single account per machine |
| `google` | OAuth | Covers Google Workspace, Drive, Calendar, Gmail |
| `slack` | OAuth | Workspace-scoped; one entry per workspace |
| `jira` | OAuth | One entry per Jira site |
| `bitbucket` | OAuth | One entry per workspace |
| `datadog` | API key | Stored in keychain |
| `pagerduty` | API key | Stored in keychain |
| `splunk` | API key | Stored in keychain |
| `google_cloud` | CLI (`gcloud`) | Project-scoped; `profile` selects a gcloud config |

For a full list of supported providers and how to set up each connection type,
see [connections.md](connections.md).

---

## See Also

- [connections.md](connections.md) — how to set up connections for each
  provider
- [Architecture: Agent Toolbelt](../architecture/agent-toolbelt.md) — deep-dive
  on enforcement layers, data model, and threat model
