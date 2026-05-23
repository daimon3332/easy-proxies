# Easy Proxies

[简体中文](README_CN.md)

Easy Proxies is a sing-box based proxy pool and node lifecycle manager. It is designed for people who import large proxy subscriptions, test every node, keep failed nodes for later retesting, and expose only usable nodes through a stable local proxy pool or per-node ports.

The WebUI is the primary workflow. It supports importing subscription URLs, raw URI lists, Base64 subscriptions, and Clash YAML, then testing nodes against `generate_204`, detecting the real exit country, renaming nodes by tag and country, promoting selected nodes into the pool, and managing ports from the browser.

## Table Of Contents

- [Highlights](#highlights)
- [How It Works](#how-it-works)
- [Quick Start](#quick-start)
- [Build From Source](#build-from-source)
- [Docker](#docker)
- [Configuration](#configuration)
- [WebUI Guide](#webui-guide)
- [Node Lifecycle](#node-lifecycle)
- [Import Formats](#import-formats)
- [Testing And Country Detection](#testing-and-country-detection)
- [Port Management](#port-management)
- [GeoIP Region Router](#geoip-region-router)
- [Management API](#management-api)
- [Supported Protocols](#supported-protocols)
- [Files And Persistence](#files-and-persistence)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

## Highlights

- **Modern WebUI workflow**: Light blue and green interface with focused pages for import, candidate nodes, pool nodes, failed nodes, ports, logs, and settings.
- **No dashboard dependency**: The WebUI starts from practical operations instead of a decorative dashboard.
- **Four import entry points**: Subscription URL, URI list, Base64 content, and Clash YAML.
- **URL auto-detection**: HTTP and HTTPS subscription URLs are fetched, then parsed as Clash YAML, Base64, or URI content according to the response body.
- **Tag based naming**: Imported nodes use a tag prefix, defaulting to `local`.
- **Country based renaming**: Country testing renames nodes to a stable pattern such as `tag-JP1`, `tag-SG2`, or `local-US3`.
- **Candidate pool workflow**: Nodes that pass speed testing become candidates first. They are not added to the runtime pool until selected and promoted.
- **Failed node retention**: Failed nodes stay in the failed list. They can be retested later instead of being discarded.
- **Batch operations**: Tables support row checkboxes, select-all, batch speed test, batch country test, batch promotion, and batch failed-node retest.
- **Automatic testing per page**: Candidate nodes, pool nodes, and failed nodes each have independent auto-test controls.
- **Fast batch testing**: Backend batch testing is concurrent, with a capped per-node timeout to avoid long serial waits.
- **Port visibility**: Pool nodes show assigned ports, and the port page shows assigned ports plus unavailable port summaries.
- **Automatic port recommendation**: The port scanner recommends a usable range based on the current pool size and the requested start port.
- **Hot reload support**: Promoting, deleting, subscription refresh, and selected settings can trigger sing-box reloads without manually editing files.
- **Wide protocol support**: VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS, SOCKS5, HTTP, and HTTPS.
- **GeoIP region routing**: Optional route endpoints such as `/jp`, `/us`, `/hk`, `/sg`, and `/other`.
- **REST API**: The browser UI is backed by HTTP APIs for automation and external tooling.

## How It Works

Easy Proxies has two related but separate layers:

| Layer | Purpose |
| --- | --- |
| Managed node store | Keeps imported nodes, their state, latency, country, tag, original name, pool status, and last error. This is persisted in `managed_nodes.json`. |
| Runtime sing-box config | Contains active nodes that are actually exposed by the local proxy pool or per-node ports. |

The important distinction is:

- **Candidate nodes** passed speed testing but are not yet active in the runtime pool.
- **Pool nodes** are active runtime nodes and have assigned ports when multi-port or hybrid mode is enabled.
- **Failed nodes** failed speed testing or were removed from the pool after a failed retest.
- **Deleted nodes** are permanently removed from the managed store and, if needed, from runtime config.

## Quick Start

### 1. Create `config.yaml`

If you are running from this repository, a `config.yaml` may already exist. If not, create a minimal one:

```yaml
mode: pool

listener:
  address: 127.0.0.1
  port: 2323
  username: username
  password: password

multi_port:
  address: 127.0.0.1
  base_port: 24000
  username: mpuser
  password: mppass

pool:
  mode: sequential
  failure_threshold: 3
  blacklist_duration: 24h

management:
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""

subscription_refresh:
  enabled: true
  interval: 24h
  timeout: 30s
  health_check_timeout: 1m
  drain_timeout: 30s
  min_available_nodes: 1

geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  auto_update_enabled: true
  auto_update_interval: 24h

log:
  output: stdout
  file: logs/easy_proxies.log
  max_size: 50
  max_backups: 3
  max_age: 7
  compress: false

nodes: []
nodes_file: ""
subscriptions: []
external_ip: ""
log_level: info
skip_cert_verify: false
```

### 2. Run

Windows PowerShell:

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
.\easy_proxies.exe -config config.yaml
```

Linux or macOS:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
./easy_proxies -config config.yaml
```

### 3. Open WebUI

Open:

```text
http://127.0.0.1:9091
```

If `management.password` is empty, no login is required. If it is set, call `/api/auth` or log in through the browser.

### 4. Import Nodes

Use **Import Nodes** in the WebUI:

1. Select import format.
2. Enter a tag prefix, for example `local`, `provider`, or `liangxin`.
3. Paste subscription URLs, URI lines, Base64 content, or Clash YAML.
4. Click import and test.
5. Passed nodes appear in **Candidate Nodes**.
6. Failed nodes appear in **Failed Nodes** and can be retested later.
7. Select candidate nodes and add them to **Node Pool** when you want them active.

## Build From Source

This project uses Go 1.24.

Recommended build:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

Windows:

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
```

Recommended full feature build:

```bash
go build -tags "with_clash_api with_utls with_quic with_grpc with_wireguard with_gvisor" -o easy_proxies ./cmd/easy_proxies
```

Build tag notes:

| Tag | Why it matters |
| --- | --- |
| `with_clash_api` | Required for sing-box Clash API integration used by monitoring and traffic APIs. |
| `with_utls` | Enables uTLS fingerprint behavior used by many modern VLESS, VMess, and Trojan nodes. |
| `with_quic` | Required for QUIC based protocols such as Hysteria2 and TUIC. |
| `with_grpc` | Enables gRPC transport support where required. |
| `with_wireguard` | Enables WireGuard support in sing-box builds. |
| `with_gvisor` | Enables gVisor support in sing-box builds. |

If Hysteria2, TUIC, or other QUIC based nodes fail with an error like `QUIC is not included in this build`, rebuild with `with_quic`.

## Docker

The repository includes `docker-compose.yml` and `start.sh`.

Linux:

```bash
chmod +x start.sh
./start.sh
```

Manual Docker Compose:

```bash
touch config.yaml nodes.txt
docker compose up -d
```

The compose file uses host networking by default. This is recommended for automatic port allocation and multi-port mode.

Important Docker notes:

- `config.yaml` and `nodes.txt` must exist as files before Docker starts.
- If a bind mount target does not exist, Docker may create a directory instead of a file.
- `start.sh` fixes that common bind-mount problem automatically.
- WebUI settings need write access to `config.yaml`.
- If settings cannot be saved, check file permissions on the host.

## Configuration

### Runtime Mode

```yaml
mode: pool
```

Supported modes:

| Mode | Behavior |
| --- | --- |
| `pool` | One local mixed proxy entry. All pool nodes share one endpoint and are selected by pool scheduling. |
| `multi-port` | Each active node gets a dedicated local port. |
| `hybrid` | Enables both pool entry and per-node ports. |

### Listener

```yaml
listener:
  address: 127.0.0.1
  port: 2323
  username: username
  password: password
```

This is the shared local mixed proxy entry for `pool` and `hybrid` mode.

Example:

```bash
curl -x http://username:password@127.0.0.1:2323 https://ipinfo.io
```

### Multi-Port

```yaml
multi_port:
  address: 127.0.0.1
  base_port: 24000
  username: mpuser
  password: mppass
```

In `multi-port` and `hybrid` mode, active pool nodes can receive individual ports. If the requested port is unavailable, Easy Proxies can move to a later available port and show the result in the WebUI.

### Pool

```yaml
pool:
  mode: sequential
  failure_threshold: 3
  blacklist_duration: 24h
```

Pool options:

| Field | Description |
| --- | --- |
| `mode` | Scheduling mode. Common values are `sequential` and `random`. |
| `failure_threshold` | Number of failures before a runtime node is considered unhealthy. |
| `blacklist_duration` | How long an unhealthy node stays blacklisted before it can be retried. |

### Management

```yaml
management:
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""
```

| Field | Description |
| --- | --- |
| `enabled` | Enables WebUI and management API. |
| `listen` | WebUI/API listen address. |
| `probe_target` | Default connectivity target used by runtime probing. |
| `password` | WebUI password. Empty means no login required. |

### Subscription Refresh

```yaml
subscription_refresh:
  enabled: true
  interval: 24h
  timeout: 30s
  health_check_timeout: 1m
  drain_timeout: 30s
  min_available_nodes: 1
```

The WebUI settings page exposes auto-refresh as day, hour, and minute inputs. It applies to subscription URLs that have been imported through **Import Nodes**. The settings page intentionally does not duplicate a large subscription URL editor.

### GeoIP

```yaml
geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: ""
  port: 0
  auto_update_enabled: true
  auto_update_interval: 24h
```

If `port` is `0`, the default GeoIP router port is used. The common default is `1221`.

### Logging

```yaml
log:
  output: stdout
  file: logs/easy_proxies.log
  max_size: 50
  max_backups: 3
  max_age: 7
  compress: false

log_level: info
```

`log.output` can be `stdout` or `file`. The WebUI log page reads recent logs from an in-memory ring buffer.

### Nodes

Static nodes can be configured directly:

```yaml
nodes:
  - name: example-node
    uri: vless://uuid@example.com:443?security=tls&type=ws&path=/path#example-node
    port: 24000
```

Or from a file:

```yaml
nodes_file: nodes.txt
```

`nodes.txt` uses one URI per line.

### Subscriptions

```yaml
subscriptions:
  - https://provider.example/api/v1/client/subscribe?token=xxx
```

Subscriptions are also maintained when imported through the WebUI subscription URL mode.

## WebUI Guide

The WebUI uses a simple page structure:

| Page | Purpose |
| --- | --- |
| Import Nodes | Import subscription URLs, URI lists, Base64 content, or Clash YAML. |
| Candidate Nodes | Nodes that passed speed testing but are not active in the pool. |
| Node Pool | Active nodes currently exposed by the runtime config. |
| Failed Nodes | Nodes that failed testing and can be retested later. |
| Port Status | Current assigned ports, unavailable port summary, and recommended port range. |
| Logs | Full-width recent runtime logs. |
| Settings | Probe target, log options, GeoIP toggle, and subscription auto-refresh interval. |

### Import Nodes

Import modes:

| Button | Input |
| --- | --- |
| Subscription URL | One or more HTTP/HTTPS subscription URLs, one per line. |
| URI Format | One proxy URI per line. |
| Base64 Format | Base64 encoded V2Ray-style subscription content. |
| Clash YAML Format | Clash or Mihomo YAML containing `proxies`. |

The subscription URL mode fetches each URL first, then detects the response content. A URL can return Clash YAML, Base64, or raw URI lines.

Import behavior:

- Tag prefix defaults to `local`.
- Imported node names initially use `tag-originalName`.
- Speed testing is run during import.
- Passed nodes go to **Candidate Nodes**.
- Failed nodes go to **Failed Nodes**.
- Import does not require a preview confirmation.
- Imported subscription URLs are saved for subscription auto-refresh.

### Candidate Nodes

Candidate nodes are usable but not yet active.

Available actions:

- Select rows with checkboxes.
- Select all visible rows.
- Click table headers to sort by that column.
- Click the same header again to reverse sort direction.
- Batch speed test selected nodes.
- Batch country test selected nodes.
- Add selected nodes to the node pool.
- Delete a node permanently.
- Enable per-page auto-test.

Failure behavior:

- If a candidate node fails speed testing, it moves to **Failed Nodes**.
- Country testing does not include speed testing.
- If you want speed validation before country testing, run speed test first.

### Node Pool

Node pool nodes are active runtime nodes.

Available actions:

- Select rows with checkboxes.
- Batch speed test selected pool nodes.
- Batch country test selected pool nodes.
- Reorder ports by country.
- Reorder ports by tag.
- Reorder ports by latency.
- Delete a node permanently.
- Enable per-page auto-test.

Failure behavior:

- If a pool node fails speed testing, it is removed from the pool and moved to **Failed Nodes**.
- Removing a pool node triggers runtime config updates and reload where applicable.

Port reorder behavior:

- **By country** groups nodes by detected country code.
- **By tag** groups nodes by import tag prefix.
- **By latency** places lower latency nodes earlier.
- Reordering affects active pool node order and therefore assigned ports.

### Failed Nodes

Failed nodes are retained for later recovery.

Available actions:

- Select rows with checkboxes.
- Select all visible rows.
- Run one-click speed test for selected failed nodes.
- Delete a node permanently.
- Enable per-page auto-test.
- Toggle whether recovered failed nodes go directly to the node pool.

Recovery behavior:

- Failed node speed test succeeds.
- Country test is run automatically for recovered failed nodes.
- If **auto add to node pool** is enabled, recovered nodes are promoted directly to the pool.
- If it is disabled, recovered nodes go to **Candidate Nodes**.

### Port Status

The port page is based on the current node pool size.

It shows:

- Multi-port listen address.
- Requested start port.
- Target node count.
- Recommended assignable port range.
- Unavailable port count and exact unavailable ports.
- Assigned port and node name for pool nodes.

Unavailable reasons:

| Reason | Meaning |
| --- | --- |
| `listener_conflict` | The port conflicts with the shared listener port. |
| `occupied_by_os` | The port is already used by the OS or another process. |
| `used_by_config` | The port is already assigned in config. |

### Logs

The log page is designed as a full-width console view. It reads recent runtime logs from the in-memory log buffer.

### Settings

Settings includes:

- External IP used by exported proxy URLs.
- Probe target.
- Skip certificate verification.
- GeoIP enable toggle.
- Log output and rotation settings.
- Subscription auto-refresh interval with day, hour, and minute inputs.
- Save and refresh subscription button.

## Node Lifecycle

The managed lifecycle is:

```text
parsed -> testing -> passed -> in_pool
                  -> failed
in_pool -> failed
failed -> testing -> passed
passed -> excluded
any visible state -> deleted
```

State meanings:

| State | Meaning |
| --- | --- |
| `parsed` | Node was parsed from an import but has not completed testing. |
| `testing` | Node is currently being tested. |
| `passed` | Node passed speed testing and is a candidate. |
| `failed` | Node failed speed testing or was removed from the pool after failing. |
| `in_pool` | Node is active in runtime config. |
| `excluded` | Node was excluded from active use. |

Name behavior:

- Before country detection: `tag-originalName`.
- After country detection: `tag-CCN`.
- Example: `liangxin-JP1`, `liangxin-SG2`, `local-US3`.
- When a node fails, its name is reset to `tag-originalName` so the failed list remains understandable.

## Import Formats

### Subscription URL

Input:

```text
https://provider.example/api/v1/client/subscribe?token=xxx
https://another-provider.example/sub
```

Behavior:

- One URL per line.
- Only `http://` and `https://` URLs are accepted.
- Response content is parsed automatically.
- Clash rules are ignored. Only nodes under `proxies` are imported.
- Subscription URLs are saved for auto-refresh.

### URI List

Input:

```text
vless://uuid@example.com:443?security=tls&type=ws&path=/path#node-1
trojan://password@example.com:443?security=tls&type=ws#node-2
ss://method:password@example.com:443#node-3
vmess://base64-json
```

Behavior:

- One URI per line.
- Supported URI schemes are listed in [Supported Protocols](#supported-protocols).
- Empty lines are ignored.

### Base64

Input:

```text
dmxlc3M6Ly8...
```

Behavior:

- Common V2Ray subscription format.
- Decoded content is expected to contain URI lines.

### Clash YAML

Input:

```yaml
proxies:
  - name: example
    type: vless
    server: example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    network: ws
    ws-opts:
      path: /path
      headers:
        Host: example.com
```

Behavior:

- Only `proxies` are imported.
- Rules, proxy groups, DNS, and other Clash config sections are ignored.
- Clash YAML can include inline JSON-style proxy objects.

## Testing And Country Detection

### Speed Test

Speed testing checks actual proxy connectivity against a `generate_204` target. It does not only validate syntax.

Backend behavior:

- A temporary sing-box instance is created for the node under test.
- The tester connects through that proxy.
- Latency is measured.
- Batch tests run concurrently.
- Per-node timeout is capped to avoid very slow total runs.

### Country Test

Country testing is separate from speed testing. It checks the real proxy exit location.

The backend tries:

1. `https://ipinfo.io/json`
2. `http://ip-api.com/json/?fields=status,countryCode,country`
3. `https://api.country.is`

Country detection updates:

- `country_code`
- `country_name`
- display name
- active runtime node name if the node is already in the pool

### Auto-Test

Each node table has its own auto-test setting:

| Page | Auto-test behavior |
| --- | --- |
| Candidate Nodes | Retests candidates. Failed candidates move to Failed Nodes. |
| Node Pool | Retests active nodes. Failed pool nodes are removed from pool and moved to Failed Nodes. |
| Failed Nodes | Retests failed nodes. Recovered nodes are country-tested and then moved to candidate or pool depending on the toggle. |

## Port Management

Ports matter in `multi-port` and `hybrid` mode.

The port system is designed around this rule:

- The user provides a preferred start port.
- Easy Proxies checks actual port availability.
- It skips unavailable ports.
- It recommends enough usable ports for the current pool size.
- It shows unavailable ports as a summary instead of pretending assigned pool ports are unusable.

API example:

```bash
curl "http://127.0.0.1:9091/api/ports/status?from=24000&count=60"
```

Response fields:

| Field | Meaning |
| --- | --- |
| `address` | Multi-port bind address. |
| `base_port` | Scan start port. |
| `target_count` | Number of nodes that need ports. |
| `recommended` | Suggested usable range and skipped ports. |
| `ports` | Port scan details. |

## GeoIP Region Router

When GeoIP routing is enabled, Easy Proxies can expose region-specific routes.

Common routes:

| Route | Meaning |
| --- | --- |
| `/jp` | Japan nodes |
| `/kr` | Korea nodes |
| `/us` | United States nodes |
| `/hk` | Hong Kong nodes |
| `/tw` | Taiwan nodes |
| `/sg` | Singapore nodes |
| `/other` | Other regions |

Example:

```bash
curl -x http://username:password@127.0.0.1:1221/jp/ https://ipinfo.io
```

The exact router listen address and port are controlled by `geoip.listen` and `geoip.port`.

## Management API

All API endpoints except `/api/auth` require authentication when `management.password` is set.

Auth header:

```http
Authorization: Bearer <token>
```

### Auth

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/auth` | Login with `{"password":"..."}` and receive a session token. |

### Import

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/import/parse` | Parse URL or pasted content into managed nodes. |
| `POST` | `/api/import/{import_id}/commit` | Commit parsed nodes and start testing. |
| `GET` | `/api/import/jobs/{job_id}` | Read import job progress. |

Parse subscription URL:

```bash
curl -X POST http://127.0.0.1:9091/api/import/parse \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"mode":"url","url":"https://provider.example/sub","tag_prefix":"local"}'
```

Parse pasted content:

```bash
curl -X POST http://127.0.0.1:9091/api/import/parse \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"mode":"content","content":"vless://...","tag_prefix":"local"}'
```

Commit all parsed nodes:

```bash
curl -X POST http://127.0.0.1:9091/api/import/<import_id>/commit \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"auto_reload":true}'
```

Commit selected parsed nodes:

```bash
curl -X POST http://127.0.0.1:9091/api/import/<import_id>/commit \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["node-id-1","node-id-2"],"auto_reload":true}'
```

### Managed Nodes

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/nodes/all` | List all managed nodes. |
| `GET` | `/api/nodes/pool` | List active pool nodes. |
| `GET` | `/api/nodes/failed` | List failed nodes. |
| `PUT` | `/api/nodes/order` | Save pool node order. |
| `POST` | `/api/managed-nodes/batch-test` | Batch speed test, country test, and optional promotion. |
| `POST` | `/api/managed-nodes/{id}/retest` | Speed test one node. |
| `POST` | `/api/managed-nodes/{id}/country` | Country test one node. |
| `POST` | `/api/managed-nodes/{id}/promote` | Add a passed candidate to the pool. |
| `POST` | `/api/managed-nodes/{id}/exclude` | Exclude a node. |
| `POST` | `/api/managed-nodes/{id}/delete` | Permanently delete a node. |
| `DELETE` | `/api/managed-nodes/{id}/delete` | Permanently delete a node. |

Batch speed test:

```bash
curl -X POST http://127.0.0.1:9091/api/managed-nodes/batch-test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["id1","id2"],"retest":true,"auto_reload":true}'
```

Batch country test:

```bash
curl -X POST http://127.0.0.1:9091/api/managed-nodes/batch-test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["id1","id2"],"country":true,"auto_reload":true}'
```

Failed node recovery directly into pool:

```bash
curl -X POST http://127.0.0.1:9091/api/managed-nodes/batch-test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["id1","id2"],"retest":true,"country":true,"promote_passed":true,"auto_reload":true}'
```

Batch response:

```json
{
  "total": 2,
  "retested": 2,
  "passed": 1,
  "failed": 1,
  "country_ok": 1,
  "country_bad": 0,
  "promoted": 1,
  "nodes": []
}
```

### Ports

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/ports/status?from=24000&count=60` | Scan ports and return recommendation. |
| `GET` | `/api/ports/status?from=24000&to=24200` | Scan explicit port range. |

### Runtime Nodes

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/nodes` | Runtime node snapshot. |
| `POST` | `/api/nodes/{tag}/probe` | Probe one runtime node. |
| `POST` | `/api/nodes/{tag}/release` | Release one node from blacklist. |
| `POST` | `/api/nodes/{tag}/blacklist` | Blacklist one runtime node. |
| `POST` | `/api/nodes/probe-all` | Probe all runtime nodes with SSE output. |

### Subscription And Settings

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/subscription/config` | Read subscription refresh config. |
| `POST` | `/api/subscription/config` | Save subscription refresh config. |
| `GET` | `/api/subscription/status` | Read subscription refresher status. |
| `POST` | `/api/subscription/refresh` | Trigger subscription refresh. |
| `GET` | `/api/settings` | Read global settings. |
| `POST` | `/api/settings` | Save global settings. |
| `POST` | `/api/reload` | Reload runtime core. |
| `GET` | `/api/export` | Export proxy URLs. |
| `GET` | `/api/logs` | Read recent logs. |

### Config Node CRUD

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/nodes/config` | List config nodes. |
| `POST` | `/api/nodes/config` | Create a config node. |
| `PUT` | `/api/nodes/config/{name}` | Update a config node. |
| `DELETE` | `/api/nodes/config/{name}` | Delete a config node. |

## Supported Protocols

| Protocol | URI scheme | Notes |
| --- | --- | --- |
| VLESS | `vless://` | Supports TLS, Reality, TCP, WS, HTTP/2, gRPC, and related fields parsed from supported formats. |
| VMess | `vmess://` | Supports common Base64 JSON VMess URI format and Clash input. |
| Trojan | `trojan://` | Supports TLS, WS, SNI, and common query parameters. |
| Shadowsocks | `ss://` | Supports SIP002 style URIs and plugin fields where parser support exists. |
| Hysteria2 | `hysteria2://`, `hy2://` | Requires `with_quic` build tag. |
| TUIC | `tuic://` | Requires `with_quic` build tag. |
| AnyTLS | `anytls://` | Supported by sing-box where build and config support exist. |
| SOCKS5 | `socks5://`, `socks://` | Direct upstream SOCKS proxy. |
| HTTP/HTTPS | `http://`, `https://` | Direct upstream HTTP proxy. |

## Files And Persistence

| File | Purpose |
| --- | --- |
| `config.yaml` | Main runtime config. WebUI settings and active pool changes can write to this file. |
| `managed_nodes.json` | Managed node store with imported nodes, state, latency, country, and pool metadata. |
| `nodes.txt` | Optional URI list loaded by `nodes_file`. |
| `GeoLite2-Country.mmdb` | GeoIP database used by region routing. |
| `logs/easy_proxies.log` | Optional rotating log file when file logging is enabled. |

Delete behavior:

- Deleting a candidate removes it from `managed_nodes.json`.
- Deleting a failed node removes it from `managed_nodes.json`.
- Deleting a pool node removes it from both managed state and runtime config, then reloads runtime where applicable.

## Troubleshooting

### `QUIC is not included in this build`

Rebuild with `with_quic`:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

### `import service not available`

This means the monitor server is running without the import service attached. Restart the current full application entrypoint:

```bash
./easy_proxies -config config.yaml
```

Do not serve the embedded WebUI from a partial test harness if you need import features.

### `snap.forEach is not a function`

This is a frontend symptom of receiving a non-array response where the UI expected a node list, often caused by API errors or stale browser code. Hard refresh the page and check `/api/nodes/all`, `/api/nodes/pool`, and `/api/nodes/failed`.

### `Cannot read properties of null (reading 'error')`

This usually means an API call failed but returned an unexpected body. Check the browser network panel or the WebUI logs page for the endpoint response.

### Imported node fails with DNS `NXDOMAIN`

The node was parsed but the upstream configuration may be invalid or provider-side DNS/SNI/Reality fields are not usable. Keep it in Failed Nodes and retest later, or delete it if you do not want it to appear again.

### Country test fails or rate limits

Country testing uses external IP lookup services through the proxy. If one service fails, the tester tries fallbacks. Temporary rate limits can still happen when testing many nodes.

### Port shown by logs differs from requested base port

Another process or listener may already be using one or more ports. Use **Port Status** to scan from your desired start port. Easy Proxies will recommend enough usable ports for the current node pool and summarize skipped ports.

### WebUI settings cannot be saved in Docker

Check host file permissions:

```bash
chmod 666 config.yaml nodes.txt
```

Also make sure `config.yaml` is a file, not a directory.

## Development

Run tests:

```bash
go test ./...
```

Run vet:

```bash
go vet ./...
```

Build recommended local binary:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

Build Docker image:

```bash
docker build -t easy_proxies:local .
```

## License

MIT License
