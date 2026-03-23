# Headless & Server Mode

## What it is

Run Huginn without a terminal UI on servers, in Docker containers, or CI/CD pipelines. Three modes cover different non-interactive use cases:

| Mode | Command | Use case |
|------|---------|---------|
| Server mode | `huginn tray --server` | Skip onboarding; serve the web UI on a remote server |
| Headless mode | `huginn --headless` | No TUI; output to stdout; ideal for scripts |
| Single-turn | `huginn --print "..."` | Run one message and exit; CI/scripting |

---

## How to use it

### Server mode

Skip the onboarding prompt and start the web UI immediately — intended for servers and Docker containers where no TTY is present.

```bash
huginn tray --server
```

The web UI is available at `http://localhost:8421` (or whatever port is configured). Use `web.bind: "0.0.0.0"` in config to expose it beyond localhost.

### Headless mode

No TUI, output goes to stdout. Useful for scripts that need to parse output.

```bash
huginn --headless --print "what does the auth middleware do?"

# JSON output for piping
huginn --headless --json --print "list all API endpoints"
```

### Single-turn mode

Run a message and exit immediately:

```bash
huginn --print "summarize internal/payment/gateway.go"
# Short form
huginn -p "summarize internal/payment/gateway.go"
```

### Docker

```dockerfile
FROM ubuntu:22.04
COPY huginn /usr/local/bin/huginn
ENV HUGINN_CLOUD_URL=https://api.huginncloud.com
EXPOSE 8421
CMD ["huginn", "tray", "--server"]
```

Build and run:
```bash
docker build -t my-huginn .
docker run -p 8421:8421 my-huginn
```

To also bind the web UI to all interfaces inside the container, add to `~/.huginn/config.json`:
```json
{ "web": { "bind": "0.0.0.0" } }
```

---

## Configuration

| Key / Variable | Where | Description |
|----------------|-------|-------------|
| `--server` | CLI flag | Skip onboarding; start immediately (use on servers/Docker) |
| `--headless` | CLI flag | No TUI; output to stdout |
| `--print` / `-p` | CLI flag | Non-interactive single-turn; exit after response |
| `--json` | CLI flag | Output JSON (headless mode) |
| `HUGINN_CLOUD_URL` | Environment variable | Override WebSocket relay URL (default: `https://api.huginncloud.com`) |
| `web.bind` | `~/.huginn/config.json` | Bind address; set to `"0.0.0.0"` to expose on network |

---

## Tips & common patterns

- **Always use `--server` in Docker** — without it, Huginn waits for onboarding input and blocks startup.
- **Set `web.bind: "0.0.0.0"` in Docker** — the default `127.0.0.1` bind address is unreachable from the Docker host. Combine with `-p 8421:8421` in your `docker run` command.
- **Use `HUGINN_CLOUD_URL` for fleet provisioning** — each server can connect to HuginnCloud automatically without a browser. See [HuginnCloud](huginncloud.md).
- **JSON output for CI** — pipe `--json --headless --print` output into `jq` for structured CI reporting.

---

## Troubleshooting

**Startup blocks on onboarding prompt**

Add `--server` to skip onboarding:
```bash
huginn tray --server
```

**Web UI not accessible from Docker host**

The default bind address is `127.0.0.1` (container-only). Set `web.bind` to expose it:
```json
{ "web": { "bind": "0.0.0.0" } }
```
Then expose the port: `docker run -p 8421:8421 my-huginn`

**No output in CI / script**

Ensure `--headless` and either `--print` or `--json` are set:
```bash
huginn --headless --json --print "run the test suite"
```
