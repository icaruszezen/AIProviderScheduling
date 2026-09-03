# 集群配置同步

集群把一个 **master** 节点的配置复制到一个或多个 **slave** 节点。配置由 master 掌握，slave 只应用收到的配置并拒绝本地配置写入。每个节点仍各自对外提供 API 服务，因此这是配置复制，不是请求路由。

默认角色是 `standalone`，行为与不配置 `cluster` 完全一致。

## 角色

| 角色 | 行为 |
|------|------|
| `standalone` | 不同步。未配置 `cluster` 时的默认值。 |
| `master` | 向已注册的 slave 推送配置并响应其拉取。配置仍可编辑。 |
| `slave` | 向 master 注册，拉取并应用其配置，配置写入返回 `403 slave_node_readonly`。 |

Home 模式（`home.enabled`）开启时集群同步整体禁用：协议端点返回 `404`。

## 配置

```yaml
cluster:
  role: master                    # standalone | master | slave
  token: "<32 位以上的密钥>"       # 集群内所有节点共用
  node-id: ""                     # 为空时首次启动生成并持久化
  master-url: ""                  # 仅 slave，例如 http://10.0.0.1:8317
  advertise-url: ""               # 仅 slave，master 推送时使用的地址
  sync-interval-seconds: 30       # slave 拉取间隔，最小 5
  heartbeat-interval-seconds: 15  # slave 心跳间隔，最小 5
```

`CLUSTER_TOKEN` 环境变量优先于 `cluster.token`，并且是下发密钥的推荐方式。该 token 不会出现在 `GET /config` 的返回中，也不会包含在导出的配置里。

### token 要求

集群协议端点不受 `remote-management.allow-remote` 限制即可访问，token 是保护整份配置导出的唯一凭据。由此有两条规则：

- 长度不足 32 位的 token 视为**未配置**。节点记录 warn 日志并保持集群同步关闭，而不是带着弱密钥运行。
- token 认证失败按来源 IP 计数，与管理密钥共用封禁记录，同一地址反复失败会被临时封禁。被封禁的地址即使之后提供了正确 token 也会收到 `403`。

### 间隔要求

两个间隔的下限都是 5 秒。修改后无需重启即可生效：间隔变化时 slave 的同步与心跳循环会被重建。

两侧间隔应保持一致。master 判定节点是否失联时使用节点自己在心跳中上报的间隔，因此 slave 用 60s 轮询不会仅因为 master 用 15s 就被误判为 stale。

### 连通性

- slave 需要能访问 `master-url`。
- master 需要能访问各 slave 的 `advertise-url` 才能推送。没有可用的 `advertise-url` 时，slave 仍能通过自身的拉取循环收敛，只是延迟为一个同步间隔而非即时。

## 哪些同步、哪些不同步

除以下每节点自有的身份字段外，配置的其余部分全部复制：

- `host`、`port`、`tls`
- `auth-dir`
- `remote-management`
- `cluster`
- `home`
- `pprof`
- `plugins.dir`（`plugins.enabled` 与插件列表会同步）

磁盘上的凭据**不同步**。`auth-dir` 是本地路径，其中的文件不会被传输，因此 OAuth 凭据需要在每个节点各自准备。写在配置文件里的 API key（`gemini-api-key`、`openai-compatibility` 等）属于配置，会同步。

## 端点

协议端点，使用集群 token 认证：

| 方法 | 路径 | 方向 |
|------|------|------|
| POST | `/v0/management/cluster/register` | slave → master |
| POST | `/v0/management/cluster/heartbeat` | slave → master |
| POST | `/v0/management/cluster/unregister` | slave → master |
| GET | `/v0/management/cluster/export` | slave → master |
| PUT | `/v0/management/cluster/apply` | master → slave |

管理端点，使用管理密钥认证：

| 方法 | 路径 | 用途 |
|------|------|------|
| GET | `/v0/management/cluster` | 当前角色与状态 |
| PATCH | `/v0/management/cluster` | 修改角色、master 地址、token、间隔 |
| GET | `/v0/management/cluster/nodes` | master 视角的节点清单 |
| POST | `/v0/management/cluster/sync` | 立即推送到所有 slave |
| DELETE | `/v0/management/cluster/nodes/:id` | 从清单中移除节点 |

`POST /cluster/sync` 会返回每个节点的结果，存在失败节点时响应 `207 Multi-Status`，避免把部分失败的同步报成成功。

## 收敛

master 先剥离本地身份字段，再按键排序序列化为规范 YAML 并计算哈希。slave 只在哈希与已应用的不同时才应用，因此稳定状态下的集群不产生磁盘写入和重载。

payload 携带 `updated_at`，slave 会拒绝比已应用版本更旧的 payload，避免一次慢推送覆盖掉 slave 期间自行拉取到的更新配置。

## slave 写保护

在 slave 上，会修改配置的管理端点返回：

```json
{ "error": "slave_node_readonly", "message": "slave node: config is synced from master" }
```

确属节点本地的操作仍然放行，因为它们要么不触及同步的配置，要么操作的是不同步的文件：

- `/v0/management/cluster` 下的全部端点（以便提升 slave 或改指向）
- 认证文件管理、OAuth 流程、`get-auth-status`
- 日志端点、`reset-quota`、`vertex/import`、`api-call`
- 插件安装（`plugin-store`）与插件卸载（`DELETE /plugins/:id`），因为二进制位于不同步的 `plugins.dir`

插件**配置**（`/plugins/:id/enabled`、`/plugins/:id/config`）属于同步的配置，仍然被拦截。

要为集群修改配置，请在 master 上操作。

## 相关：同凭据重试

`provider-retry-count` 与集群无关，但常被一起调整，其语义容易被误解：

- 同一凭据上的重试是**立即**发起的，不做退避，也不读取上游返回的 `Retry-After`。
- 因此它只适用于「同一凭据立刻重发确有成功机会」的场景，例如上游的瞬时故障。
- 对真正在限流的上游，调大该值只会更快消耗配额，应改为依赖切换到其他凭据的故障转移。

## 关于 `docker-compose.cluster.yml`

仓库根目录的 `docker-compose.cluster.yml` 早于本特性，指的是 **Home 模式**集群（`HOME_JWT`），是另一套机制。它不是 master/slave 配置同步的部署示例，而且两者互斥：Home 模式启用时集群协议端点会被禁用。
