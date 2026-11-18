# linapro-employee-core

与身份提供方无关的员工身份与认证核心，由考勤、问卷、360 测评和飞书推送插件共用。员工通过飞书登录；业务数据引用内部`EmpID`（而非飞书`open_id`），因此将来切换身份提供方（例如钉钉）时无需改动业务表。

## 它拥有什么

- **员工表**：内部`EmpID`代理主键，按租户隔离，存活行上`(tenant_id, moka_employee_no)`唯一，`status`支持实时离职。员工是否存在以 Moka HCM 为准。
- **飞书身份映射（`lark_identity`）**：`(lark_app_id, lark_open_id) → emp_id`，附`lark_user_id`与`id_source`（`own`/`assoc`）。同一人横跨多个飞书企业时每个身份一行，均指向同一`emp_id`。只有`own`行可用于登录。
- **部门归属**：保留表与读模型，但目前不写入数据，预留给将来的部门数据来源。
- **写入网关**：`UpsertEmployee`、`MarkDeparted`、`UpsertLarkIdentity`。员工表与身份映射表的所有写入都经此网关。
- **登录与会话**：`POST /emp/login`（飞书 code 换票，未登记或已离职的员工一律拒绝，不自动建档）与`GET /emp/me`。员工 JWT 使用独立签名密钥，有效期 2 小时，无刷新。`EmpAuth()`每请求验签并重新校验`status`，注入`EmpContext{EmpID, TenantID}`。
- **目录同步任务**：从 Moka 拉取全部员工，按顶层部门映射到租户；再按工号匹配每个已配置飞书应用的通讯录写入`lark_identity`，并对账离职。
- **`cap/empcap`**：其他源码插件通过 go.mod`replace`引入的公开契约，包括`EmpAuth()`、`EmpContext`、员工批量读取、人员字段`open_id`解析与仅限`own`身份的推送寻址。
- **后台「通讯录管理 → 员工列表」**：分页查看同步落库的员工花名册（含离职），并排查「某人为什么登录不了 / 收不到推送」。可编辑的只有同步不写的字段：手工绑定/解绑飞书身份、关联后台账号；姓名、工号、租户、在职状态由 Moka 同步维护，后台只读。**不提供新增与删除**——手工加的员工没有 Moka 工号锚点，删掉的下一轮又会被同步回来。权限点：`linapro-employee-core:employee:list` / `:query` / `:edit`。

## 配置

| 配置键 | 用途 |
|---|---|
| `plugin.linapro-employee-core.lark_apps` | 飞书应用平铺数组：`app_id`、`secret_key`、可选`name`（后台展示名，留空则界面只显示`app_id`）、可选`assoc`、可选`identity_app_id`（见下） |
| `plugin.linapro-employee-core.emp_jwt_secret` | 员工 JWT 签名密钥；缺失时登录与`EmpAuth`失败关闭 |
| `plugin.linapro-employee-core.moka.*` | 同步任务使用的 Moka HCM 凭据 |
| `plugin.linapro-employee-core.sync_interval_minutes` | 同步任务在每小时的第几分钟执行（默认`0`） |
| sys_config`plugin.linapro-employee-core.dept_tenant_map` | JSON`{"顶层部门名": tenant_id}`；为空时同步中止 |

`lark_apps`登记两类应用，以有无`identity_app_id`区分：

| 类型 | `identity_app_id` | 行为 |
|---|---|---|
| 身份应用 | 不填 | 通讯录同步、写`lark_identity`、出现在后台「绑定飞书身份」下拉；登录按`open_id`查员工 |
| 免登应用 | 某个身份应用的`app_id` | 只用于员工端免登（如「360测评助手」网页应用），同步与后台下拉都看不到它；登录用它自己的凭据换票取`user_id`，按（`identity_app_id`，`user_id`）查员工，`user_id`为空即拒绝 |

`identity_app_id`须指向同一清单里的身份应用（不能指向自身或另一个免登应用），`app_id`不得重复，
否则登录与同步都返回`PLUGIN_EMPLOYEE_CORE_LARK_APPS_CONFIG_INVALID`。免登应用须在飞书后台开通
「获取用户 user ID」权限。

密钥不落库。消息推送用的应用凭据不在这里，属于`linapro-lark-push`（推送应用可以同时在这里登记为免登应用）。

## 它不拥有什么

数据权限 / scope（由各消费方决定员工可见范围）、飞书 WS 组织变更事件、部门数据来源，以及飞书↔org-core 的部门树映射 / 主管子树。

完整设计见`openspec/changes/linapro-employee-core/`。
