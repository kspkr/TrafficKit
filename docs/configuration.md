# Configuration

Settings come from, lowest to highest priority:

1. built-in defaults
2. the config file
3. `TRAFFICKIT_*` environment variables
4. command-line flags

## Config file

JSON, looked up at:

| OS | Path |
|---|---|
| Windows | `%AppData%\TrafficKit\config.json` |
| macOS | `~/Library/Application Support/traffickit/config.json` |
| Linux | `$XDG_CONFIG_HOME/traffickit/config.json` (usually `~/.config/...`) |

A missing file is fine. `--config <path>` uses a specific file, which must
exist. Unknown keys are an error, so typos don't pass silently.

```json
{
  "dataDir": "C:\\Users\\you\\AppData\\Roaming\\TrafficKit",
  "proxy": {
    "bind": "127.0.0.1",
    "port": 8877,
    "allowRemote": false,
    "autoStart": true
  },
  "https": {
    "intercept": true,
    "passthrough": ["*.apple.com", "bank.example.com"]
  },
  "capture": {
    "maxBodyBytes": 4194304,
    "maxExchanges": 50000
  },
  "log": {
    "level": "info",
    "format": "json"
  }
}
```

| Key | Default | Meaning |
|---|---|---|
| `dataDir` | the config folder | Where the certificate authority and launcher profiles live. |
| `proxy.bind` | `127.0.0.1` | Address the proxy listens on. An IP or `localhost`. |
| `proxy.port` | `8877` | Proxy port. |
| `proxy.allowRemote` | `false` | Required to bind a non-loopback address such as `0.0.0.0`. Doing so lets anyone who can reach this machine use the proxy and have their traffic captured. Needed for testing phones on your LAN. |
| `proxy.autoStart` | `true` | Start the proxy when the engine starts. |
| `https.intercept` | `true` | Decrypt HTTPS with the local CA. Off means every `CONNECT` is relayed untouched. |
| `https.passthrough` | `[]` | Hosts never decrypted: exact names or `*.example.com`. For apps that pin certificates. |
| `capture.maxBodyBytes` | 4 MiB | Bytes kept per request body and per response body. Beyond this, data is still forwarded but not stored. `0` stores no bodies. |
| `capture.maxExchanges` | 50 000 | Exchanges kept in memory; the oldest are dropped first. |
| `log.level` | `info` | `debug`, `info`, `warn`, `error`. Logs never contain header values, bodies or query strings. |
| `log.format` | `json` | `json` or `text`. Logs go to stderr. |

## Environment variables

| Variable | Overrides |
|---|---|
| `TRAFFICKIT_DATA_DIR` | `dataDir` |
| `TRAFFICKIT_HTTPS_INTERCEPT` | `https.intercept` |
| `TRAFFICKIT_PROXY_BIND` | `proxy.bind` |
| `TRAFFICKIT_PROXY_PORT` | `proxy.port` |
| `TRAFFICKIT_PROXY_ALLOW_REMOTE` | `proxy.allowRemote` (`true`/`false`) |
| `TRAFFICKIT_PROXY_AUTOSTART` | `proxy.autoStart` |
| `TRAFFICKIT_MAX_BODY_BYTES` | `capture.maxBodyBytes` |
| `TRAFFICKIT_MAX_EXCHANGES` | `capture.maxExchanges` |
| `TRAFFICKIT_LOG_LEVEL` | `log.level` |
| `TRAFFICKIT_LOG_FORMAT` | `log.format` |

## Flags

`traffickit proxy` and `traffickit serve` both accept `--config`, `--bind`,
`--port` and `--log-level`. `serve` also takes:

| Flag | Meaning |
|---|---|
| `--api-port` | Control API port on 127.0.0.1. Default: any free port. |
| `--no-proxy` | Don't start the proxy until asked through the API. |
| `--allow-origin` | Extra origin allowed to call the API (repeatable). |
| `--dev-state` | Also write the ready line, token included, to this file. Development only. |
| `--exit-on-stdin-close` | Exit when stdin closes; used when running as a child process. |

Changing the proxy address from the Settings screen lasts until the app
quits; it does not rewrite the config file.
