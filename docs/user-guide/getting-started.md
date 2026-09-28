# Getting started

## 1. Open TrafficKit

The app opens on **Connect**. The sidebar shows **Proxy running** and its
address, `127.0.0.1:8877` by default. If the port is taken, pick another in
**Settings → Proxy**.

## 2. Connect a source

**A browser.** Click Chrome, Edge, Brave, Chromium or Vivaldi (whichever you
have). A new window opens on a page that confirms HTTPS interception is
working. It has its own temporary profile, so your usual logins, extensions
and history aren't in it, and it's deleted when the window closes. Type a URL
into **Open at** first to start somewhere specific.

Chromium shows a grey bar saying you're using an unsupported command-line
flag. That's the flag that makes this one window trust TrafficKit; it's
expected and only affects that window.

**A terminal.** **New terminal** opens PowerShell (Terminal.app on macOS)
with proxy and certificate variables set. Anything you run there goes
through TrafficKit, HTTPS included, for tools that read those variables:
curl, Node 22.12+, Python (requests, httpx, urllib, pip), git, Deno, Cargo,
the AWS CLI. Programs that only use the OS certificate store (Go on Windows
and macOS, .NET, Java) will fail with a certificate error instead; their
requests show up as `tls_untrusted`.

Everything you launch is listed under **Active sources**. Close it there, or
just close its window. TrafficKit closes anything still open when it quits.

**Anything else.** Point the app or device at the proxy address yourself.
For HTTPS it also needs to trust the TrafficKit certificate: **Show
certificate file** opens it, and devices using the proxy can download it
from `http://<proxy address>/certificate`. Only install it on machines you
control, and remove it when you're done.

## 3. Inspect

Switch to **Traffic** and click an exchange (or use ↑/↓):

- **Overview**: URL, status, protocol, sizes, timing at a glance
- **Request / Response**: headers, query parameters, cookies, body, and a
  reconstructed raw message
- **Timing**: DNS, connect, TLS, send, wait, download

Bodies are decoded (gzip, br, deflate, zstd). JSON is pretty-printed; switch
to Text or Hex above the body.

WebSocket connections show up as `websocket` in the Type column with a
running message count. Their **Messages** tab lists every message as it
happens: arrows mark sent and received, you can filter by direction or search
payloads, and clicking a message shows it in full. Compressed WebSockets
(`permessage-deflate`, which browsers use by default) are decompressed for
you.

Press `/` to filter. Every word must match somewhere in method, host, path,
status or content type; prefix a word with `-` to exclude it, e.g.
`api -404`. All shortcuts are listed in **Settings**.

## When HTTPS fails

If a client doesn't trust TrafficKit's certificate, you'll see a red
`CONNECT` row explaining that the client rejected it. Either launch the
client from Connect, trust the certificate in it, or, for apps that pin
their certificates (banking apps, many mobile apps), add the host to
**Settings → HTTPS → Passthrough hosts** so it's relayed without decryption.

To stop decrypting entirely, switch **Settings → HTTPS** to **Tunnel only**.

## Your data

Captured traffic lives in the engine's memory and disappears when you quit.
Nothing is uploaded. With HTTPS decryption on you will see passwords, tokens
and cookies in plain text, so capture only traffic you're allowed to see.
