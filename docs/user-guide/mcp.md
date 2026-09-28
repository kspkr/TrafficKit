# Using TrafficKit with AI assistants (MCP)

`traffickit mcp` is a [Model Context Protocol](https://modelcontextprotocol.io)
server. Add it to Claude Code, Claude Desktop, Cursor, VS Code or any other
MCP client and the assistant can read what TrafficKit captured, search it,
and open intercepted browser windows to reproduce problems.

## Setup

Open **Settings → AI assistants (MCP)** in TrafficKit. It shows the exact
command for your install, with the full path to `traffickit`:

```sh
# Claude Code
claude mcp add traffickit -- "C:\Users\you\AppData\Local\TrafficKit\traffickit.exe" mcp
```

For clients configured with JSON (Claude Desktop, Cursor, VS Code, ...):

```json
{
  "mcpServers": {
    "traffickit": { "command": "C:\\...\\traffickit.exe", "args": ["mcp"] }
  }
}
```

Keep the TrafficKit app open while you use it. The MCP server connects to
the running app (it finds it through `engine.json` in TrafficKit's data
folder), so the assistant sees the same traffic you do, and the status bar
shows **AI connected: <assistant>** while it's in use.

## What the assistant can do

| Tool | |
|---|---|
| `get_status` | Proxy address, HTTPS decryption on/off, how much is captured, which browsers can be launched |
| `list_traffic` | Newest-first list, filterable by host, method, status, failures or free text |
| `get_exchange` | Headers, decoded bodies, timings and errors for one exchange |
| `search_traffic` | Find text in URLs, headers and bodies |
| `wait_for_request` | Wait for a matching request, e.g. while you click through a page |
| `launch_browser` | Open an isolated, intercepted browser window at a URL |
| `close_source` | Close a window TrafficKit opened |
| `proxy_settings` | Proxy URL, certificate and environment variables for routing the assistant's own commands |
| `clear_traffic` | Delete everything captured |

Things to try:

- *"Open example-app.test/login in a TrafficKit browser. I'll log in; tell me
  why the login request fails."*
- *"Which requests to api.stripe.com returned errors in the last few minutes,
  and what did the responses say?"*
- *"Run the test suite through the TrafficKit proxy and list every external
  host it calls."*

## Privacy

Everything a tool returns goes to the assistant's provider. By default
TrafficKit redacts before anything leaves:

- `Authorization`, `Proxy-Authorization`, `Cookie` and `Set-Cookie` values
  (cookie and scheme names are kept), and any header whose name looks like a
  secret (`*token*`, `*api-key*`, `*session*`, ...)
- query parameters and form fields such as `token`, `access_token`, `code`,
  `password`, `api_key`
- JSON fields with those names at any depth, including JSON embedded in
  strings
- anything shaped like a JWT or a `Bearer ...` credential

Each result lists what was hidden. Binary bodies are never sent, and text
bodies are cut to 8 000 characters unless the assistant asks for more.

If you need the real values (for example to debug a signature), register the
server with `mcp --no-redact`.

Captured pages come from the internet and can contain text written to
manipulate an AI ("ignore your instructions and ..."). TrafficKit tells the
assistant to treat traffic as data, but keep an eye on what it does,
especially with tools that act (`launch_browser`, `clear_traffic`).

## Without the app

`traffickit mcp --standalone` runs its own engine and proxy inside the MCP
process, for CI or headless machines. It uses the same config
(`proxy.port`, `https.*`) and prints the proxy address to stderr.
