# HuginnCloud

## What it is

An optional cloud service that lets you access your Huginn agents from any machine or browser. Huginn connects to HuginnCloud via a WebSocket relay — your agents and models stay local, but commands can be sent remotely from any device. Once connected, your machine appears as "Online" in the HuginnCloud dashboard and you can open a chat session from any browser.

HuginnCloud is a separate service from Huginn itself. You need an account at `app.huginncloud.com` to use it.

---

## How to use it

### Connect from the web UI

1. Run `huginn tray` and open `http://localhost:8421`
2. Click the profile icon in the bottom-left corner
3. Click **"Connect to Huginn Cloud"**
4. A browser window opens at `app.huginncloud.com/connect`
5. Approve the connection in the browser
6. Return to the web UI — the profile icon now shows a green dot and "Connected"

### Connect from the CLI

```bash
huginn connect
```

This runs the same browser-based flow without launching the web UI.

### What "connected" means

- Green dot in the profile popover
- Satellite WebSocket is active between your machine and `api.huginncloud.com`
- Your machine is listed as "Online" in the HuginnCloud dashboard
- You can open a chat session from any browser via HuginnCloud

---

## Configuration

| Key / Variable | Where | Default | Description |
|----------------|-------|---------|-------------|
| `HUGINN_CLOUD_URL` | Environment variable | `https://api.huginncloud.com` | Override the WebSocket relay endpoint (for self-hosted HuginnCloud) |
| `cloud.url` | `~/.huginn/config.json` | `"https://app.huginncloud.com"` | Override the HuginnCloud web app URL |

For fleet/headless deployments, pass the token via environment:

```bash
HUGINN_CLOUD_TOKEN=hgn_abc123 huginn tray --server
```

See [Headless Mode](headless.md) for server and Docker deployments.

---

## Tips & common patterns

- **Connection persists across restarts** — once approved, the token is stored in the OS keychain. Restarting Huginn reconnects automatically without re-opening the browser.
- **Token survives reinstalls** — the OS keychain entry is not removed when you reinstall Huginn. After reinstalling, the existing token is reused and the connection restores on the next launch.
- **Multiple machines** — each machine registers separately. You'll see each one in the HuginnCloud dashboard with its own "Online" / "Offline" status.
- **Fleet deployments** — for provisioning many machines via MDM or CI, use the `HUGINN_CLOUD_TOKEN` env var with a pre-issued fleet token instead of the browser flow. See [Headless Mode](headless.md).

---

## Troubleshooting

**"Connecting..." spinner never resolves**

The browser window either didn't open or the approval wasn't completed. Check:
1. `app.huginncloud.com` is reachable from your machine
2. You clicked "Approve" (not "Deny") in the browser tab
3. The callback to localhost was not blocked by a firewall or browser extension

If the spinner is still stuck after a confirmed approval, restart Huginn — the connection state will restore.

**"Not connected" after restart**

A stale or invalid token was detected and automatically cleared. Click "Connect to Huginn Cloud" again to re-register with a fresh token.

**`websocket: bad handshake` in logs**

The stored token was rejected by the HuginnCloud API (expired or revoked). Huginn clears the stale token automatically and logs a warning. Click Connect to re-register.

**HuginnCloud account questions**

Account management, billing, and team features are documented in the HuginnCloud documentation (separate service). Huginn's docs only cover the connection and configuration steps.
