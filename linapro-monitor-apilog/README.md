# linapro-monitor-apilog

`linapro-monitor-apilog` is the official LinaPro source plugin for interface request logging. It
automatically records complete requests and responses for **non-admin** interface calls and offers a
back-office query page. Records are append-only.

English | [简体中文](README.zh-CN.md)

## What gets recorded

The plugin binds a global middleware on `/*` and evaluates five conditions after the handler has
run. All of them must hold:

| Condition | Detail |
|---|---|
| Route owner is a plugin | Source plugins come from the host route marker, dynamic plugins from route metadata. Host-owned routes (such as host login) are skipped |
| No back-office username in context | Requests with an admin username belong to `linapro-monitor-operlog` and are not duplicated here |
| It is an interface request | Static assets and `OPTIONS` preflight are skipped |
| Route owner is not excluded | `exclude_route_owners`, default `[linapro-lark-push]` |
| Path is not excluded | `exclude_paths`, empty by default |

The effect: **employee-facing endpoints are recorded with no business-code changes.**

### Boundaries with neighbours

- **`linapro-monitor-operlog`** partitions on "has an admin username". The two are not strictly
  complementary: an admin issuing a GET to an endpoint without the `operLog` tag is recorded by
  neither. This gap is accepted for now.
- **`linapro-lark-push`** is **not recorded**. That plugin already logs its caller, business number,
  Feishu `code` and `req_id`, and explicitly decided not to persist message bodies; storing the body
  here would overturn that decision. Setting `exclude_route_owners: []` records it anyway — that is a
  deliberate administrator choice.
- **Missing key vs explicit empty list**: a missing `exclude_route_owners` key falls back to the
  built-in default `[linapro-lark-push]`, it is **not** treated as an empty list. Forgetting one key
  must not silently start persisting message bodies.

## Caller identity convention

After successful authentication, middleware writes a JSON string into the request context variable
`linapro.request.caller`:

```json
{"kind":"emp","id":"5","tenant":1,"name":"张三","no":"GZ001198"}
```

| Field | Meaning |
|---|---|
| `kind` | `emp` (employee) or `app_key` (external system) |
| `id` | EmpID for `emp`, the app_key for `app_key`. **The only machine-readable correlation key** |
| `tenant` | Tenant ID (integer); omitted when there is no tenant semantics |
| `name` / `no` | Employee name and Moka employee number as a **write-time snapshot**; omitted when unavailable. Not written for `app_key` |

The log plugin splits it into `caller_kind`, `caller_id`, `caller_tenant`, `caller_name` and
`caller_no`. When nothing is written those columns stay empty and the record is still stored; a parse
failure only blanks those five columns and logs a WARNING carrying the request_id — the record is
never dropped.

**Why snapshots:** a log answers "who called this at that time". After a rename, an employee-number
change, or departure, querying the employee table yields "what this EmpID is called now", not "who it
was then".

### Current writers

| Writer | Coverage |
|---|---|
| `empcap.EmpAuth()` (`linapro-employee-core`) | Employee endpoints of both `linapro-employee-core` and `linapro-eval-360`; it is the same function |
| `Login` controller of `linapro-employee-core` | Written on successful login. Failures do not write: we genuinely do not know who it was |

`linapro-lark-push` is not recorded and therefore not wired. `linapro-attend-poc` is throwaway
verification code that will be deleted and is not wired; a future real attendance plugin authenticates
through `empcap.EmpAuth()` and is covered automatically.

### Wiring a new plugin

1. In your auth middleware, after success call
   `r.SetCtxVar("linapro.request.caller", "<JSON string>")`;
2. **Do not** import this plugin, and do not import another plugin's cap package — the string
   convention keeps both sides free of dependencies. When this plugin is absent the variable has no
   reader;
3. Define the key with a local constant plus a comment pointing at this convention, and add a unit
   test asserting the constant equals `linapro.request.caller`.

## Masking

Masking happens **before persistence**; unmasked secrets never reach the database:

- Request headers `Authorization`, `Cookie`, `Set-Cookie`, `X-Signature`, `X-Emp-Token` become
  `[REDACTED]`;
- JSON request and response bodies are walked recursively; keys (case-insensitive) matching
  `password`, `passwd`, `secret`, `secret_key`, `token`, `access_token`, `refresh_token`,
  `signature`, `authorization` are replaced;
- Form and multipart fields follow the same rules, as do query-string parameters;
- Additional field names can be added through `mask_fields`.

**`code` is not masked**: the host unified response envelope uses `code` for the integer business
error code, so masking it would blank the error code of every response. The Feishu pre-auth code is
single-use and valid for roughly five minutes, so the risk of storing it is low.

Uploads record only field name, filename, Content-Type and size — **never file content**. Binary and
streamed responses are recorded as placeholders.

## Permissions

| Permission | Detail |
|---|---|
| `monitor:apilog:list` | List, **without** headers, request body or response body |
| `monitor:apilog:query` | Detail, including complete masked payloads |

Records are platform-level audit data: the caller is an employee or an external system and belongs to
no department. Department data scope is therefore **not** applied; access is controlled by permissions
alone. `monitor:apilog:query` bypasses every business permission, so a holder can read 360-assessment
free-text answers. This is an accepted trade-off — holders are platform administrators.

## Append-only (operations must-read)

- **No delete endpoint, no page entry, no `Delete` method** — the code is simply not written.
- **No cleanup job, no retention**: 360-assessment and attendance records are kept forever too.
- **Uninstall keeps the table**: the uninstall SQL is comments only. On reinstall, new columns
  converge through each `ADD COLUMN IF NOT EXISTS` in the install SQL.
- **Partitions only prune queries, they are never used for bulk deletion**: `DROP PARTITION` and
  detaching are forbidden. A daily job keeps six months of partitions ahead; it only creates.

### The real safety net is a `pg_dump` backup

The application connects as the `postgres` superuser, so **database privileges stop no mistakes**:

| Approach | Feasible | Why |
|---|---|---|
| `REVOKE DELETE` | No | Superusers bypass every privilege check |
| `BEFORE DELETE` trigger | Partly | Cannot stop `DISABLE TRIGGER` or `DROP TABLE`; `TRUNCATE` skips row triggers |
| Scheduled `pg_dump` | **The only net** | The only thing that survives "dropped the database" and disk loss |

Configure a scheduled backup for this table before going live, and verify one restore.

## Storage and partitioning

`plugin_linapro_monitor_apilog` is range-partitioned monthly on `request_at`, with partitions
named `..._p{YYYYMM}`.

- `id` comes from a sequence and the primary key is `(id, request_at)`: PostgreSQL 14 does not support
  identity columns on partitioned tables, and a partitioned table's unique constraint must include the
  partition key.
- `request_at` is the **request start time**, captured by the middleware before `Next()` and written
  explicitly; it shares its baseline with `cost_ms`. Async queueing therefore never turns time filtering
  into write-time semantics, and a request spanning a month boundary lands in the month it started.
- `created_at` is the write time. Like every other table in the repository it is filled by the
  framework, never assigned in code, and plays no part in filtering or partitioning.
- **The detail lookup must carry `requestAt`**: `WHERE id = ?` scans every partition. The endpoint
  rejects a missing value instead of quietly getting slower.
- **No DEFAULT partition**: once rows land in DEFAULT, creating the real partition for that range
  fails, and the only fix is moving rows out — that is a delete, which conflicts with append-only.
  A missing partition instead makes inserts fail with
  `no partition of relation found for row`: one ERROR log carrying the request_id, business requests
  unaffected, and inserts recover as soon as the partition is created.

Storage grows forever with no exit. Watch table size monthly and agree on a growth plan with
operations; the only lever to reduce writes is the exclusion list.

## Configuration

```yaml
plugin:
  linapro-monitor-apilog:
    # Excluded by route owner. A missing key falls back to the built-in [linapro-lark-push];
    # an explicit [] records everything.
    exclude_route_owners: [linapro-lark-push]
    # Exclude single high-frequency endpoints (health checks) by path prefix.
    exclude_paths: []
    # Additional field names to mask, merged with the built-in list.
    mask_fields: []
    # Operations alert channel for partition failures; any missing field means silent.
    alert: *ops_alert
```

The exclusion lists are read per request, so edits need no restart. Alert credentials live under
`_anchors.ops_alert` and share the same literal values with `linapro-lark-push`, but **each plugin
reads its own key** — the anchor removes duplication, it does not decide who reads what.

## Failure handling

- **A failed write never affects the business request**: writes run asynchronously in `grpool`; a
  failure logs an ERROR with the request_id and leaves the already-sent response untouched.
- **An unreadable request body does not discard the record**: when downstream closed the body or the
  route body limit already fired, the body is recorded as `[BODY 不可读]` while path, caller and
  status survive.
- **Partition failures alert**: an ERROR plus a `apilog_partition_missing` push through the shared
  `linapro-lark-sdk/larkalert` library, deduplicated to one alert per 30 minutes.

## Development

```bash
# Generate dao/do/entity (the table must exist first)
cd apps/lina-plugins/linapro-monitor-apilog && make dao

# Unit tests
go test ./apps/lina-plugins/linapro-monitor-apilog/...
```
