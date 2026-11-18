# linapro-monitor-apilog

`linapro-monitor-apilog` 是 LinaPro 的接口请求日志官方源码插件：它自动记录**非管理后台**接口的
完整请求与响应，并提供后台查询页。记录只增不删。

[English](README.md) | 简体中文

## 记录范围

插件在 `/*` 上绑定全局中间件，请求处理完成后按以下条件依次判定，**全部满足才记录**：

| 条件 | 说明 |
|---|---|
| 归属插件 | 路由归属某个插件。源码插件取宿主标记，动态插件取路由元数据。宿主自有路由（如宿主登录）不记录 |
| 没有后台登录身份 | 有后台用户名的请求由 `linapro-monitor-operlog` 负责，本插件不重复记录 |
| 是接口请求 | 静态资源、`OPTIONS` 预检不记录 |
| 归属插件不在排除名单 | 配置项 `exclude_route_owners`，默认排除 `linapro-lark-push` |
| 路径不在排除名单 | 配置项 `exclude_paths`，默认为空 |

结果就是：**员工端接口（飞书免登、员工令牌）自动被记录，不需要改任何业务代码。**

### 与相邻能力的边界

- **`linapro-monitor-operlog`（操作日志）**：两者按「有没有后台用户名」二分，不是严格互补。
  管理员用 GET 访问未打 `operLog` 标签的接口时，两份日志都没有——本期接受这个空洞。
- **`linapro-lark-push`（消息推送）**：默认**不记录**。该插件已自行留痕调用方、业务单号、飞书
  `code` 与 `req_id`，并明确决定不保存消息正文；日志插件再存一遍正文等于推翻该决定。
  若要临时改变，把 `exclude_route_owners` 显式改成 `[]`——那是管理员有意的选择。
- **键缺失 vs 显式空**：`exclude_route_owners` 键缺失时回落到内置默认值 `[linapro-lark-push]`，
  **不是**当成空列表。漏抄一个键不能让消息正文静默开始落库。

## 调用方标识约定

鉴权中间件通过后，把一个 JSON 字符串写进请求上下文变量 `linapro.request.caller`：

```json
{"kind":"emp","id":"5","tenant":1,"name":"张三","no":"GZ001198"}
```

| 字段 | 说明 |
|---|---|
| `kind` | `emp`（员工）或 `app_key`（外部系统） |
| `id` | `emp` 时为 EmpID；`app_key` 时为 app_key。**唯一的机器关联键** |
| `tenant` | 租户 ID（整数）；无租户语义时省略 |
| `name` / `no` | 员工姓名与 Moka 工号，**写入时快照**；拿不到时省略。`app_key` 时不写 |

日志插件把它拆成 `caller_kind`、`caller_id`、`caller_tenant`、`caller_name`、`caller_no` 五列。
未写入时各列为空，记录照常写入；解析失败只留空这五列并记一条带 request_id 的 WARNING，不丢记录。

**为什么用姓名与工号快照**：日志回答的是「当时谁调用了这个接口」。员工改名、换工号、离职之后，
现查员工表得到的是「这个 EmpID 现在叫什么」，不是「当时是谁」。

### 当前写入点

| 写入点 | 覆盖范围 |
|---|---|
| `empcap.EmpAuth()`（`linapro-employee-core`） | `linapro-employee-core` 与 `linapro-eval-360` 的员工端，是同一个函数 |
| `linapro-employee-core` 的登录控制器 `Login` | 登录成功时写入。登录失败不写：此时确实不知道是谁 |

`linapro-lark-push` 默认不被记录，因此不接入。`linapro-attend-poc` 是将被删除的最小验证代码，不接入；
未来的真实考勤插件经 `empcap.EmpAuth()` 鉴权，自动覆盖。

### 新插件如何接入

1. 在你的鉴权中间件里，鉴权通过后调用 `r.SetCtxVar("linapro.request.caller", "<JSON 字符串>")`；
2. **不要**为此 import 本插件，也不要 import 任何业务插件的 cap 包——用字符串约定即可，
   两边保持零依赖。日志插件未安装时，这个变量没有任何读者；
3. 用本地常量写键名并附注释指向本约定；加一条单测断言常量值等于 `linapro.request.caller`。

## 脱敏

落库**之前**完成，未脱敏的敏感值不进入数据库：

- 请求头 `Authorization`、`Cookie`、`Set-Cookie`、`X-Signature`、`X-Emp-Token` 整体替换为 `[REDACTED]`；
- JSON 请求体与响应体递归处理，键名（不区分大小写）命中 `password`、`passwd`、`secret`、
  `secret_key`、`token`、`access_token`、`refresh_token`、`signature`、`authorization` 时替换；
- 表单与 multipart 字段按同一规则处理；查询串参数同理；
- 可通过配置 `mask_fields` 追加字段名。

**不脱敏 `code`**：宿主统一响应结构的字段就叫 `code`（整数业务错误码），加进去会把每一条响应的
错误码都变成占位。飞书预授权码是一次性的、有效期约 5 分钟，落库风险低。

上传文件只记字段名、文件名、Content-Type 与大小，**不存文件内容**；二进制与流式响应只记占位。

## 权限点

| 权限码 | 说明 |
|---|---|
| `monitor:apilog:list` | 列表，**不含**请求头、请求体与响应体 |
| `monitor:apilog:query` | 详情，含完整的请求头、请求体与响应体 |

记录是平台级审计数据，调用方是员工或外部系统、不属于任何部门，因此**不接入部门数据范围**，
只以权限点控制访问。

`monitor:apilog:query` 不经过任何业务权限，因此持有它的管理员能看到 360 评估的作答正文。
这是已确认的取舍：持有该权限的都是平台管理员。

## 记录只增不删（运维必读）

- **没有删除接口、没有页面入口、service 层没有 Delete 方法**——不是藏起来，是根本不写这段代码。
- **没有清理任务、没有保留期**：360 评估与考勤的记录同样永久保存。
- **卸载不删表**：卸载 SQL 只有注释。卸载后重装，新增列靠安装 SQL 里的
  `ADD COLUMN IF NOT EXISTS` 收敛。
- **分区只用于查询剪枝，不用来批量删除**：禁止 `DROP PARTITION` / detach 分区。分区由每日任务
  提前创建 6 个月，任务只创建、不删除。

### 真正的兜底是 `pg_dump` 备份

应用是用 `postgres` 超级用户连库的，**数据库权限挡不住误操作**：

| 手段 | 是否可行 | 原因 |
|---|---|---|
| `REVOKE DELETE` | 不可行 | 超级用户绕过一切权限检查 |
| `BEFORE DELETE` 触发器 | 部分有效 | 防不了 `DISABLE TRIGGER` 与 `DROP TABLE`；`TRUNCATE` 也不触发行级触发器 |
| 定期 `pg_dump` | **唯一兜底** | 只有它能挡住「误删整库」与「磁盘损坏」 |

上线前必须为这张表配置定期备份，并验证一次恢复。

## 存储与分区

表 `plugin_linapro_monitor_apilog` 按 `request_at` 做**月度范围分区**，分区名
`..._p{YYYYMM}`。

- `id` 由序列生成，主键为 `(id, request_at)`——PostgreSQL 14 不支持分区表上的 identity 列，
  且分区表的唯一约束必须包含分区键。
- `request_at` 是**请求开始时间**，由中间件在 `Next()` 之前取值并显式写入，与 `cost_ms` 同一基准。
  异步排队的延迟不会让时间筛选变成写入时间语义，跨月边界的请求也不会落进下一个月的分区。
- `created_at` 是写库时间，与仓库其他表一致由框架自动写入，代码不手动赋值，也不参与筛选与分区。
- **详情查询必须带 `requestAt`**：`WHERE id = ?` 会扫描全部分区。接口在缺少该参数时直接报
  参数错误，而不是悄悄变慢。
- **不建 DEFAULT 分区**：一旦有行落进 DEFAULT，之后再为同一范围建正式分区会失败，修复只能把行
  移出去（= 删除），与「只增不删」冲突。漏建分区的后果是写入报
  `no partition of relation found for row`——一条带 request_id 的 ERROR 日志，业务请求不受影响，
  补建分区后立即恢复。

存储会持续增长且没有出口。按月观察表大小，与运维确认扩容节奏；减少写入的手段只有排除名单。

## 配置

```yaml
plugin:
  linapro-monitor-apilog:
    # 按归属插件排除。键缺失时回落到内置默认值 [linapro-lark-push]；显式 [] 表示全量记录。
    exclude_route_owners: [linapro-lark-push]
    # 按路径前缀排除单条高频接口（如健康检查）。
    exclude_paths: []
    # 追加脱敏的字段名，与内置名单合并。
    mask_fields: []
    # 分区补齐失败时的运维告警通道；三项缺一则静默不告警。
    alert: *ops_alert
```

排除名单每次请求读取，改配置无需重启。告警凭据登记在配置文件的 `_anchors.ops_alert`，
与 `linapro-lark-push` 共享同一份字面值，但**各读自己的键**——锚点只解决「写一遍」，
不解决「读哪个键」。

## 失败处理

- **写库失败不影响业务请求**：写入在 `grpool` 里异步执行，失败只记一条带 request_id 的 ERROR
  日志，绝不改变已发出的响应。
- **请求体读不出来也不丢整条记录**：下游关闭了 Body、或路由请求体上限已触发时，记
  `[BODY 不可读]`，路径、调用方、状态码都还在。
- **分区补齐失败会告警**：记 ERROR 并经共享库 `linapro-lark-sdk/larkalert` 推送
  `apilog_partition_missing`，同一告警 30 分钟内只发一条。

## 开发

```bash
# 生成 dao/do/entity（需先建表）
cd apps/lina-plugins/linapro-monitor-apilog && make dao

# 单测
go test ./apps/lina-plugins/linapro-monitor-apilog/...
```
