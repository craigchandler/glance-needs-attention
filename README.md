# glance-needs-attention

A small Go helper for [Glance](https://github.com/glanceapp/glance) that polls JSON status endpoints and combines their alerts into one widget. It normally runs as a native systemd service beside Glance, listening on **127.0.0.1:8795**.

Alerts are sorted critical → warning → info. With no alerts, the widget says **No issues requiring attention**. Failed requests, invalid source settings and partial-data errors appear separately under **Data source issues**; an unavailable source does not stop other sources from being checked. An empty alert list does not guarantee that every source is reachable.

The service itself requires **no credentials**. Upstream helpers manage their own authentication. No current adapter adds authentication headers or supports credentials embedded in URLs; a future adapter would need to document any credential requirements explicitly.

## Install from a GitHub Release (recommended)

Use a Linux host with systemd, `sudo`, `curl`, `sha256sum`, and the usual account management tools (`getent`, `useradd`, `groupadd`). On Arch Linux:

```bash
sudo pacman -S --needed curl coreutils shadow
```

Install directly from GitHub using the steps below. No repository clone, Go compiler, version selection, or support bundle is required. These URLs become available after the repository and first release are published.

### 1. Download and install the latest binary

Check your architecture with `uname -m`: `x86_64` uses `amd64`; `aarch64` uses `arm64`.

For **amd64**:

```bash
curl -fL -o needs-attention \
  https://github.com/craigchandler/glance-needs-attention/releases/latest/download/needs-attention-linux-amd64
```

For **arm64**, use this download instead:

```bash
curl -fL -o needs-attention \
  https://github.com/craigchandler/glance-needs-attention/releases/latest/download/needs-attention-linux-arm64
```

Verify the downloaded binary against the release checksum (architecture is detected automatically):

```bash
(
  set -eu
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo 'Supported architectures: amd64 and arm64' >&2; exit 1 ;;
  esac
  CHECKSUM_FILE=$(mktemp)
  trap 'rm -f "$CHECKSUM_FILE"' EXIT
  trap 'exit 1' HUP INT TERM
  curl -fL -o "$CHECKSUM_FILE" \
    https://github.com/craigchandler/glance-needs-attention/releases/latest/download/SHA256SUMS
  CHECKSUM=$(awk -v asset="needs-attention-linux-$ARCH" '$2 == asset {print $1}' "$CHECKSUM_FILE")
  test -n "$CHECKSUM"
  printf '%s  needs-attention\n' "$CHECKSUM" | sha256sum --check -
)
```

Continue only if the checksum reports `needs-attention: OK`. Checksums come from the same release publisher and are not independent signatures. If a new release appeared between downloads, download the binary and checksum again.

Install it and remove the download:

```bash
sudo install -m 0755 needs-attention /usr/local/bin/needs-attention
rm needs-attention
/usr/local/bin/needs-attention --version
```

For an existing running installation, follow [Upgrade](#upgrade) below instead of replacing the running binary directly.

### 2. Create the service account and install configuration

Create the dedicated unprivileged account and its directory:

```bash
getent group needsattention >/dev/null || sudo groupadd --system needsattention
id needsattention >/dev/null 2>&1 || sudo useradd --system --gid needsattention \
  --home-dir /var/lib/needs-attention --shell /usr/bin/nologin needsattention
sudo install -d -m 0700 -o needsattention -g needsattention /var/lib/needs-attention
```

Download the configuration and optional environment file from GitHub. This block preserves existing files, including symlinks, and cleans up its temporary downloads:

```bash
(
  set -eu
  DOWNLOAD_DIR=$(mktemp -d)
  trap 'rm -rf "$DOWNLOAD_DIR"' EXIT
  trap 'exit 1' HUP INT TERM
  BASE_URL=https://raw.githubusercontent.com/craigchandler/glance-needs-attention/main
  if ! sudo test -e /etc/needs-attention.json && ! sudo test -L /etc/needs-attention.json; then
    curl -fL "$BASE_URL/config.example.json" -o "$DOWNLOAD_DIR/config.json"
    sudo install -m 0640 -o root -g needsattention \
      "$DOWNLOAD_DIR/config.json" /etc/needs-attention.json
  fi
  if ! sudo test -e /etc/needs-attention.env && ! sudo test -L /etc/needs-attention.env; then
    curl -fL "$BASE_URL/.env.example" -o "$DOWNLOAD_DIR/config.env"
    sudo install -m 0640 -o root -g needsattention \
      "$DOWNLOAD_DIR/config.env" /etc/needs-attention.env
  fi
)
```

Edit the configuration to match your upstream helpers. Disable or remove example sources you do not run:

```bash
sudoedit /etc/needs-attention.json
```

The service listens on `127.0.0.1:8795` by default. It currently keeps state in memory and requires no persistent data files.

### 3. Install the systemd unit and start the service

Download and install the unit:

```bash
(
  set -eu
  UNIT_FILE=$(mktemp)
  trap 'rm -f "$UNIT_FILE"' EXIT
  trap 'exit 1' HUP INT TERM
  curl -fL -o "$UNIT_FILE" \
    https://raw.githubusercontent.com/craigchandler/glance-needs-attention/main/systemd/needs-attention.service
  sudo install -m 0644 -o root -g root \
    "$UNIT_FILE" /etc/systemd/system/needs-attention.service
)
sudo systemctl daemon-reload
sudo systemctl enable --now needs-attention
systemctl status needs-attention --no-pager
```

The unit runs as `needsattention:needsattention` with the documented systemd hardening. Put local unit customisations in `sudo systemctl edit needs-attention` drop-ins so reinstalling the unit preserves them.

### 4. Verify the service

```bash
curl --fail http://127.0.0.1:8795/health
curl --fail http://127.0.0.1:8795/status
curl --fail --include http://127.0.0.1:8795/widget
```

`/health` reports whether a refresh completed within five refresh intervals; HTTP 200 does not imply all sources are healthy. `/status` returns alerts, their count and overall status, source errors and the last completed refresh time. `/widget` returns escaped HTML and Glance's widget headers. Polling is sequential, with a timeout per source; initial startup waits for the first refresh.

## Status API

`GET /status` returns HTTP 200 with `Content-Type: application/json` and the current aggregate state:

| Field | Meaning |
| --- | --- |
| `alerts` | Array of current alert objects; always `[]` when clear, never `null`. Each object retains `source`, `severity` and `message`. |
| `alertCount` | Integer number of active alert entries; always equals the length of `alerts`. |
| `status` | One of `healthy`, `warning` or `critical`, derived from the current alerts. |
| `refreshedAt` | Time the aggregate state was last refreshed, in the existing RFC 3339 format with fractional seconds and a timezone offset. |
| `errors` | Existing optional array of source errors; omitted when there are none. |

With no alerts, `status` is `healthy`. Any `critical` alert makes it `critical`; otherwise any `warning` alert makes it `warning`. Informational (`info`) alerts are included in `alerts` and `alertCount` but leave the overall status `healthy`. Individual alert severities and their critical → warning → info ordering are unchanged. The count is of alert objects, not any underlying counts mentioned in their messages.

Healthy example:

```json
{
  "alerts": [],
  "alertCount": 0,
  "status": "healthy",
  "refreshedAt": "2026-10-06T11:12:35.300422598+10:30"
}
```

Alerting example:

```json
{
  "alerts": [
    {
      "source": "Backups",
      "severity": "critical",
      "message": "Archive backup failed"
    },
    {
      "source": "Storage",
      "severity": "warning",
      "message": "Volume is 90% full"
    }
  ],
  "alertCount": 2,
  "status": "critical",
  "refreshedAt": "2026-10-06T11:12:35.300422598+10:30"
}
```

Source errors remain separate from alert severity: `status: "healthy"` does not guarantee that every source is reachable. `/health` continues to check refresh progress independently of alerts, and `/widget` retains its existing HTML and headers. The API changes are additive except for empty `alerts` changing from `null` to `[]`.

## Configuration

Default configuration path: `/etc/needs-attention.json`. Set `CONFIG_FILE` in the optional `/etc/needs-attention.env`, or pass `--config /path/to/config.json` when running directly. The command-line option takes precedence. Restart after changes; configuration is read at startup.

| Setting | Default | Purpose |
| --- | --- | --- |
| `title` | `Needs Attention` | Glance widget title |
| `listenAddr` | `127.0.0.1:8795` | HTTP listen address |
| `refreshInterval` | `1m` | Positive Go duration between polls |
| `httpTimeout` | `5s` | Positive Go duration limiting each source request |
| `sources` | none | Array of sources to check |

Each source has `name` (display label), `type`, `url` (HTTP or HTTPS JSON endpoint), optional `enabled` (defaults to true), and adapter-specific `settings`. Names, ports, paths and thresholds are configurable. No source is compiled into the main service.

See [config.example.json](config.example.json) for all adapters and [config.generic-example.json](config.generic-example.json) for a standalone generic setup.

| Adapter | Input and behavior |
| --- | --- |
| `backup-status` | Reads `reports` and `error`; reports failed, warning or stale backups. Supports `friendlySources` mappings and per-condition severities. |
| `github-status` | Reads `counts`, `failedRuns` and `error`; shows security alerts, failed runs, review requests and assigned issues. Categories can be disabled; partial errors stay separate. |
| `qnap-status` | Reads `volume` and `disks`; compares usage and health against configurable thresholds and healthy status lists. |
| `azure-cost` | Reads `summary.monthToDate`, `summary.currency` and `error`; warns at `monthToDateWarn` when greater than zero. Set `defaultCurrency` if the source omits currency (legacy adapter fallback: `AUD`). |
| `generic-json` | Applies configurable rules to other JSON producers without adding Go code or a specialised adapter. |

The specialised adapters expect the corresponding helper's status schema, not the raw vendor API. Example ports are only suggested local endpoints. Remote HTTP(S) sources are also supported.

## Generic JSON rules

Paths are dot-separated and support numeric array indexes, such as `disks.0.usedPercent`. An empty path refers to the current document or item. Operators:

- `eq`, `ne`: equality/inequality (also accepts equivalent textual representations).
- `gt`, `gte`, `lt`, `lte`: numeric comparisons, including numeric strings.
- `contains`: substring matching against the value's textual representation.
- `truthy`, `falsy`: boolean-style checks. Null, false, numeric zero, and empty/`"0"`/`"false"` strings are falsy; other objects and arrays are truthy, including empty ones.

```json
{
  "name": "Example Service",
  "type": "generic-json",
  "url": "http://127.0.0.1:8800/status",
  "settings": {
    "rules": [
      {
        "path": "disk.usedPercent",
        "op": "gte",
        "value": 90,
        "severity": "warning",
        "message": "Disk usage is {{value}}%"
      }
    ]
  }
}
```

`{{value}}` substitutes the matched value. `{{fieldName}}` substitutes a top-level field from the current document/item. These are text substitutions, not executable templates, and the widget escapes the result using Go `html/template`.

`forEach` expands an array into individual alerts. Paths then refer to each item; `severityPath` reads its severity. Supported severities are `critical`, `warning`, and `info`; missing/invalid extracted severities fall back to the rule's severity, then `warning`.

For example, a certificate status helper can expose an `attention` array:

```json
{
  "name": "Certificates",
  "type": "generic-json",
  "url": "http://127.0.0.1:8796/status",
  "settings": {
    "rules": [
      {
        "forEach": "attention",
        "path": "status",
        "op": "truthy",
        "severityPath": "status",
        "message": "{{name}}: {{message}}"
      }
    ]
  }
}
```

Missing, null, or empty `forEach` collections produce no alerts. A non-array collection, missing rule path, unsupported operator or nonnumeric comparison produces a source error.

## Glance integration

Add this widget to your Glance configuration (also in [glance/needs-attention.yml](glance/needs-attention.yml)):

```yaml
- type: extension
  url: http://127.0.0.1:8795/widget
  cache: 1m
  allow-potentially-dangerous-html: true
```

This assumes native Glance on the same host. If Glance runs in a container, its loopback address is separate; configure networking deliberately before changing the helper's bind address. Source strings are escaped even though the extension permits HTML.

## Homepage integration

For Homepage running on the same host/network namespace, the summary fields can be mapped directly:

```yaml
- Needs Attention:
    description: Aggregated alerts
    widget:
      type: customapi
      url: http://127.0.0.1:8795/status
      refreshInterval: 10000
      mappings:
        - field: alertCount
          label: Alerts
        - field: status
          label: Status
        - field: refreshedAt
          label: Checked
          format: relativeDate
```

If Homepage runs in a container, use an endpoint reachable from that container. The detailed alerts remain available in the same response for other consumers.

## Upgrade

Download and verify the latest binary for your architecture using step 1 above, then stop the service before replacing the executable:

```bash
sudo systemctl stop needs-attention
sudo install -m 0755 needs-attention /usr/local/bin/needs-attention
rm needs-attention
sudo systemctl start needs-attention
/usr/local/bin/needs-attention --version
systemctl status needs-attention --no-pager
curl --fail http://127.0.0.1:8795/health
curl --fail http://127.0.0.1:8795/status
```

Your existing `/etc/needs-attention.json` and `/etc/needs-attention.env` are not replaced. Check the release notes for configuration or unit changes; repeat step 3 if the unit needs updating. Back up local configuration before editing it.

For a specific release or rollback, replace `releases/latest/download/` in the binary and checksum URLs with `releases/download/v0.1.0/` (using the desired published tag). For matching configuration and unit files, replace `main` in their raw GitHub URLs with that tag.

When upgrading a previous root-running installation, ensure the service user can read the JSON file. For the default path:

```bash
sudo chown root:needsattention /etc/needs-attention.json
sudo chmod 0640 /etc/needs-attention.json
```

Custom configuration paths must also be readable by the service account and accessible under systemd's sandbox (home directories are hidden). Use `/etc` for configuration.

## Troubleshooting

```bash
journalctl -u needs-attention -n 100 --no-pager
journalctl -u needs-attention -f
systemctl cat needs-attention
sudo -u needsattention test -r /etc/needs-attention.json
```

- A failed start usually means invalid JSON/durations, unreadable configuration, or a port already in use. Check the journal.
- Source failures appear in `/status` and the widget's **Data source issues** section. Check upstream units and their configured `/status` endpoints. Polling continues after failures and recovers on later refreshes.
- Responses must contain one JSON document of at most **4 MiB**. HTTP errors show the status code; request and JSON errors omit raw URLs, response bodies and numeric field values.
- `/health` can remain healthy while sources fail. It checks polling progress, not completeness of the upstream data.
- Longer source lists may need a larger refresh interval because polling is sequential. A timeout applies to each source independently.

## Security

The listener defaults to loopback and has no authentication or TLS. Keep it local, including `/health` and `/status`; use a suitably authenticated reverse proxy if remote access is required. The service account has no capabilities, cannot log in, and uses a hardened systemd filesystem sandbox. Network access remains enabled so it can query local or remote helpers.

Configuration is trusted administrator input: it controls outbound URLs and which fields become visible. Only configure trusted upstreams. Standard HTTP redirects are followed, outbound requests have a configured timeout, and inbound connections have read/header/write/idle timeouts. The HTTP client retains Go's standard proxy environment behavior, so review any proxy variables you add to the unit.

No hard-coded credentials are needed or included. Fetch errors are sanitised, but intentionally displayed alerts and upstream `error` fields can still contain sensitive information supplied by producers. Keep secrets out of those fields and out of configuration examples. There are no HTTP file-serving or filesystem-path endpoints. Go `html/template` handles widget escaping.

## Development and releases

Use a currently supported Go toolchain (module language minimum: Go 1.22). There are no third-party Go dependencies.

```bash
go test ./...
go vet ./...
go build ./...
bash scripts/test-install-systemd.sh
go build -o needs-attention ./cmd/needs-attention
sudo sh scripts/install-systemd.sh ./needs-attention
```

The optional `scripts/install-systemd.sh /path/to/binary` installer accepts a locally built or downloaded binary when run from a source checkout or the release support bundle. It creates the account and directories, preserves existing JSON and environment files, installs the binary and unit, and reloads systemd. It does not start or restart the service automatically.

The local binary and `dist/` are ignored by Git. Keep real deployment configuration outside the repository.

Pushing a `v*` tag triggers [.github/workflows/release.yml](.github/workflows/release.yml). It runs tests and vet, builds standalone Linux amd64 and arm64 binaries with `CGO_ENABLED=0`, embeds the tag in `--version`, and publishes:

- `needs-attention-linux-amd64`
- `needs-attention-linux-arm64`
- `needs-attention-systemd.tar.gz` (installer, unit, examples, Glance snippet, README and licence)
- `SHA256SUMS`

After reviewing and pushing the initial commit, a maintainer can publish the first release with:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions must be enabled and permitted to create releases. Release builds use the stable Go toolchain via [actions/setup-go](https://github.com/actions/setup-go) and publish with [GitHub CLI](https://cli.github.com/manual/gh_release_create).

## Licence

[MIT](LICENSE), Copyright (c) 2026 Craig Chandler.
