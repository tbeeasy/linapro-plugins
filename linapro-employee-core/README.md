# linapro-employee-core

Provider-agnostic employee identity and authentication core, shared by the
attendance, survey, 360-evaluation and Lark-push plugins. Employees sign in via
Lark (Feishu); business data references the internal `EmpID` (never the Lark
`open_id`), so a future identity-provider switch (for example DingTalk) does
not touch business tables.

## What it owns

- **Employee table** — internal `EmpID` surrogate key, tenant-scoped,
  unique on live `(tenant_id, moka_employee_no)`, with `status` for real-time
  offboarding. Moka HCM is the source of truth for whether an employee exists.
- **Lark identity mapping (`lark_identity`)** — `(lark_app_id, lark_open_id) →
  emp_id`, plus `lark_user_id` and `id_source` (`own` / `assoc`). One person
  across several Lark enterprises gets one row per identity, all pointing at the
  same `emp_id`. Only `own` rows can be used to sign in.
- **Department membership** — table and read model are kept but not populated
  yet; reserved for a future department source.
- **Write gateway** — `UpsertEmployee`, `MarkDeparted`, `UpsertLarkIdentity`.
  All writes to the employee and identity tables go through here.
- **Login and session** — `POST /emp/login` (Lark code exchange, rejects
  unregistered or departed employees, never auto-provisions) and `GET /emp/me`.
  Employee JWT uses an independent signing key, 2h TTL, no refresh. `EmpAuth()`
  verifies the token and re-checks `status` on every request, injecting
  `EmpContext{EmpID, TenantID}`.
- **Directory sync job** — pulls all employees from Moka, maps the top-level
  department to a tenant, then matches each configured Lark app's contacts by
  employee number to fill `lark_identity`, and reconciles departures.
- **`cap/empcap`** — the public contract other source plugins import via a
  go.mod `replace` directive: `EmpAuth()`, `EmpContext`, batch employee reads,
  person-field `open_id` resolution, and own-only push addressing.
- **Admin "Contacts → Employees" page** — paginated view of the synced roster
  (departed employees included), meant for diagnosing "why can't this person
  sign in / receive pushes". Only fields the sync never writes are editable:
  manual Lark identity bind/unbind and the linked system account. Name, employee
  number, tenant and employment status are owned by the Moka sync and read-only.
  There is **no create or delete**: a hand-made employee has no Moka employee
  number anchor, and a deleted one comes back on the next sync. Permission codes:
  `linapro-employee-core:employee:list` / `:query` / `:edit`.

## Configuration

| Key | Purpose |
|---|---|
| `plugin.linapro-employee-core.lark_apps` | Array of Lark identity apps: `app_id`, `secret_key`, optional `assoc` |
| `plugin.linapro-employee-core.emp_jwt_secret` | Employee JWT signing key; login and `EmpAuth` fail closed when missing |
| `plugin.linapro-employee-core.moka.*` | Moka HCM credentials used by the sync job |
| `plugin.linapro-employee-core.sync_interval_minutes` | Minute of each hour at which the sync job runs (default `0`) |
| sys_config `plugin.linapro-employee-core.dept_tenant_map` | JSON `{"top-level department name": tenant_id}`; sync aborts when empty |

Secrets are never stored in the database. Message-push app credentials are not
here; they belong to `linapro-lark-push`.

## What it does NOT own

Data permission / scope (each consumer decides what an employee may see), Lark
WS org-change events, the department data source, and the Lark ↔ org-core
department-tree mapping / manager subtree.

See `openspec/changes/linapro-employee-core/` for the full design.
