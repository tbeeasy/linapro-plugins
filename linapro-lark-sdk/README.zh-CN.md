# linapro-lark-sdk

飞书（Lark / Feishu）出站访问的共享 Go 库，由一方插件（recruit-pipeline、
moka-report-sync、eval-360）经 go.mod `replace` 指令导入。它**不是插件**——
没有 `plugin.yaml`、不注册宿主钩子、不带 SQL 或菜单。

## 它拥有什么

- **`larkhttp`** —— 包裹飞书官方 SDK v3 的自定义 `HttpClient`，统一处理
  **HTTP 429 限流退避**。官方 SDK 内置重试只覆盖「拨号失败」与「token 失效」；
  本包读取 `x-ogw-ratelimit-reset`（回退到 `Retry-After`，再回退到带抖动的指数
  退避）后重试。因为飞书 SDK 的 token 请求与业务请求走同一个 `HttpClient`，
  token 刷新被限流时同样会自动退避重试。
- **`larkbitable`** —— 多维表格记录读写：批量 `list`/`get`/`create`/`update`/
  `batch_create`/`batch_update`，含分页、按字段类型的值编码，以及 `Persons`
  旁路（消费方把预解析好的 `open_id` 集合直接交给 SDK 序列化写入）。
- **`larkcontact`** —— 通讯录枚举（本租户 own + 关联组织 assoc），返回用户带
  `own`/`assoc` `Source` 标记，按 `open_id` 去重。
- **`larkim`** —— IM 出站消息：向接收者（`open_id` / `user_id` / `email` /
  `chat_id`）发送交互卡片（`msg_type=interactive`）或纯文本（`msg_type=text`），
  复用 `larkhttp` 的 429 退避。token 管理交官方 SDK。另提供 `BuildTemplateCard`
  拼装引用飞书卡片模板的 `content` JSON（模板 ID、版本名与模板变量），与
  `BuildTextContent` 把纯文本严格编码为 text 消息的 `content`（引号/反斜杠/换行/
  emoji 都走 `encoding/json` 转义；空文本与超长文本返回错误而非截断，上限
  `MaxTextRunes` = 2000 字符）。文件与图片：`UploadFile`（`im/v1/files`，返回 `file_key`，
  上限 `MaxFileBytes` = 30 MB）与 `UploadImage`（`im/v1/images`，返回 `image_key`，上限
  `MaxImageBytes` = 10 MB），再经 `SendFile` / `SendImage`（`msg_type=file` / `image`）发送。
  key 归属上传它的应用，须用即将发送的那个应用凭据上传。`FileTypeOf` 按文件名推导飞书
  `file_type`，`IsUploadableImage` 按内容识别可上传的图片。
- **`utils`** —— 子包共享的小型格式化辅助。

## 边界约束

- 只做**出站**调用。MUST NOT 实现事件订阅或回调接收。
- 依赖方向在库内（`larkim` → `larkhttp`、`larkbitable` → `larkhttp`）。MUST NOT
  引入 `lina-core`。（`larkhttp` 在源码层 import 了 `lina-core/pkg/logger` 用于
  429 退避日志，经 `go.work` 解析；其余子包不新增 `lina-core` 引用。）

## 消费方

| 消费插件 | 使用 |
|----------|------|
| `linapro-recruit-pipeline` | `larkbitable`（写结论/人员）、`larkhttp` |
| `linapro-moka-report-sync` | `larkbitable`（写报表人员）、`larkhttp` |
| `linapro-eval-360` | `larkim`（一评价者一张卡推送） |
| `linapro-employee-core` | `larkcontact`（通讯录枚举）、`larkhttp` |
| `linapro-lark-push` | `larkim`（文本、文件与图片推送） |

飞书凭据解析刻意**不放进本 SDK**：各插件读各自的配置键（身份应用 →
`plugin.linapro-employee-core.lark_apps`，推送应用 → `plugin.linapro-lark-push.push_groups`），
不需要一份需要长期同步的共享配置包。

抽取设计见 `openspec/changes/archive/2026-09-20-extract-shared-lark-sdk/`。
