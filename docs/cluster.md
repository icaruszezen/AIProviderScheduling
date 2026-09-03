# Cluster Configuration Sync

A cluster keeps one **master** node's configuration replicated onto one or more **slave** nodes. The master owns the config; slaves apply what they receive and reject local config writes. Every node still serves API traffic on its own, so this is config replication, not request routing.

The default role is `standalone`, which behaves exactly as a node without any cluster settings.

## Roles

| Role | Behaviour |
|------|-----------|
| `standalone` | No sync. Default when `cluster` is absent. |
| `master` | Publishes config to registered slaves and answers their pulls. Config remains editable. |
| `slave` | Registers with a master, pulls and applies its config, and rejects config writes with `403 slave_node_readonly`. |

Cluster sync is disabled entirely when Home mode (`home.enabled`) is on: the protocol endpoints answer `404`.

## Configuration

```yaml
cluster:
  role: master                    # standalone | master | slave
  token: "<32+ character secret>" # shared by every node in the cluster
  node-id: ""                     # generated and persisted on first start when empty
  master-url: ""                  # slave only, e.g. http://10.0.0.1:8317
  advertise-url: ""               # slave only, the URL the master pushes to
  sync-interval-seconds: 30       # slave pull interval, minimum 5
  heartbeat-interval-seconds: 15  # slave heartbeat interval, minimum 5
```

`CLUSTER_TOKEN` overrides `cluster.token` and is the preferred way to distribute the secret, since the token is never returned by `GET /config` and never included in an exported config.

### Token requirements

The cluster protocol endpoints are reachable without `remote-management.allow-remote`, so the token is the only thing guarding a full config export. Two rules follow from that:

- A token shorter than 32 characters is treated as **not configured**. The node logs a warning and cluster sync stays off rather than running behind a weak secret.
- Failed token attempts are counted per source IP and share the management-key ban tracker, so repeated failures from one address are locked out for a time. A banned address gets `403` even when it later presents the correct token.

### Interval requirements

Both intervals are clamped to a minimum of 5 seconds. Changing them takes effect without a restart: the slave's sync and heartbeat loops are rebuilt when the values change.

Set the intervals consistently. The master marks a node stale using the interval the node itself reports in its heartbeat, so a slave polling every 60s is not flagged stale just because the master polls every 15s.

### Reachability

- A slave needs to reach `master-url`.
- The master needs to reach each slave's `advertise-url` to push. Without a usable `advertise-url`, a slave still converges through its own pull loop, just on the sync interval instead of immediately.

## What syncs and what does not

Everything in the config is replicated **except** the per-node identity fields, which each node keeps as its own:

- `host`, `port`, `tls`
- `auth-dir`
- `remote-management`
- `cluster`
- `home`
- `pprof`
- `plugins.dir` (`plugins.enabled` and the plugin list do sync)

Credentials on disk are **not** synced. `auth-dir` is a local path and its files are never transferred, so OAuth credentials must be provisioned on each node. API keys that live in the config file (`gemini-api-key`, `openai-compatibility`, and so on) are part of the config and therefore do sync.

## Endpoints

Protocol endpoints, authenticated with the cluster token:

| Method | Path | Direction |
|--------|------|-----------|
| POST | `/v0/management/cluster/register` | slave → master |
| POST | `/v0/management/cluster/heartbeat` | slave → master |
| POST | `/v0/management/cluster/unregister` | slave → master |
| GET | `/v0/management/cluster/export` | slave → master |
| PUT | `/v0/management/cluster/apply` | master → slave |

Management endpoints, authenticated with the management key:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/v0/management/cluster` | Current role and status |
| PATCH | `/v0/management/cluster` | Change role, master URL, token, intervals |
| GET | `/v0/management/cluster/nodes` | Roster as seen by the master |
| POST | `/v0/management/cluster/sync` | Push to every slave now |
| DELETE | `/v0/management/cluster/nodes/:id` | Drop a node from the roster |

`POST /cluster/sync` reports per-node results and answers `207 Multi-Status` when some nodes failed, so a partially failed sync is not reported as success.

## Convergence

The master strips local identity, canonicalizes the config to YAML with sorted keys, and hashes it. A slave applies a payload only when its hash differs from what it already applied, so a steady cluster does no disk writes and no reloads.

Payloads carry `updated_at`, and a slave rejects one older than what it has already applied. That keeps a slow push from overwriting a newer config the slave pulled in the meantime.

## Slave write protection

On a slave, config-mutating management endpoints answer:

```json
{ "error": "slave_node_readonly", "message": "slave node: config is synced from master" }
```

Operations that are genuinely node-local remain allowed, because they either do not touch the synced config or act on files that are not synced:

- everything under `/v0/management/cluster` (so a slave can be promoted or repointed)
- auth file management, OAuth flows, and `get-auth-status`
- log endpoints, `reset-quota`, `vertex/import`, `api-call`
- plugin install (`plugin-store`) and plugin uninstall (`DELETE /plugins/:id`), since the binaries live in the unsynced `plugins.dir`

Plugin *configuration* (`/plugins/:id/enabled`, `/plugins/:id/config`) is part of the synced config and stays blocked.

To edit config for the cluster, change it on the master.

## Related: same-credential retries

`provider-retry-count` is unrelated to clustering but often tuned alongside it, and its semantics are easy to misread:

- Retries against the same credential are issued **immediately**. There is no backoff and the upstream's `Retry-After` header is not honored for these attempts.
- It is therefore useful only where an immediate resend on the same credential has a real chance of succeeding, such as transient upstream faults.
- Against an upstream that is genuinely rate limiting, raising this value just burns the quota faster. Rely on failover to another credential instead.

## Note on `docker-compose.cluster.yml`

The `docker-compose.cluster.yml` file in the repository root predates this feature and refers to **Home mode** clustering (`HOME_JWT`), which is a different mechanism. It is not a deployment example for master/slave config sync, and the two are mutually exclusive: the cluster protocol endpoints are disabled whenever Home mode is enabled.
