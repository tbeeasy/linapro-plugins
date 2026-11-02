# linapro-eval-360

面向管理层 **360 度绩效测评**的源码插件。以评价关系（谁评谁）为脊柱，
支持项目→问卷→被评估人→评价关系四层结构、角色权重与发布校验、Excel 全量导入
与关系编辑器、二维题目可见性、飞书一键推送，以及面向 HR 的按答案/按得分两张
明细导出。

## 一期范围（当前）

- **建卷** —— 仅四种题型：单选 / 单行文本 / 多行文本 / 矩阵量表（砍掉多选、
  评分）。
- **矩阵评价项评语** —— 每个评价项（列）列头带 `⋮` 操作栏：删除本列 / 允许评语 /
  左移 / 右移（只改草稿，随题目保存落库）。「允许评语」为开关，开启后三个子项可同时
  生效：必填（仅提交时校验——选中该列则评语不得为空）、提示语（输入框 placeholder）、
  预设填空（一行一个候选 + 是否允许自定义）。员工端选中该列时评语框长在单选钮下方；
  单选选项不提供提示语与预设。
- **编辑三策略** —— 存在作答的问卷支持三策略：① 保留答案（白名单：仅改文案/
  新增题/新增选项/调排序；禁改分值、删题、删选项、删答案），② 删除
  答案（清空全部作答并重置关系完成态后任意改），③ 复制问卷（深拷贝结构 +
  被评估人→评价关系树、不拷答案；新件未发布、不自动推送、原件不锁定）。
- **角色与权重** —— 无内置角色；角色由导入关系的「角色」列动态生成，默认权重 0。
  发布前校验每角色权重 > 0 且和 = 100%；想让某角色不计分就别把它导进关系。
- **评价关系** —— 唯一键下沉到问卷级
  `(project_id, questionnaire_id, evaluatee_id, evaluator_emp_id)`。
  全量 Excel 导入是唯一批量入口，仅发布前允许；发布后追加走关系编辑器弹窗。两者都**只录入姓名**、不录入工号：
  姓名须跨租户唯一对应一名在职员工；有两名及以上在职同名员工（离职的不算）或找不到时整次拒绝并点名问题姓名。
  导入按表头识别列，导出文件（保留两个工号列便于核对）可原样再导入。
- **发布 / 发送** —— 问卷级发布（未发布谁都看不到），单份发布即向该问卷评价者推送
  卡片。项目级「一键发送」= 推送全部未发布问卷并按问卷判定发布（已发布跳过、幂等、不重推）。`end_at` 项目级统一、问卷继承。已作废为派生
  （未填且 now > end_at），不落库、无 cron；提交入口按 now > end_at 即时拒。
- **两张明细导出** —— 两个独立按钮（按答案 / 按得分），各直出单个 Excel（不打包
  ZIP），挂问卷级「评估报告」菜单，只导已提交作答。固定前导列：
  `序号 | 提交时间 | 所用时间 | 关系角色 | 被评价对象`。
  - **按答案表**：全部题原文；矩阵一题一列（行即被评估人，取所选评价项）；选项级评语与
    矩阵评价项评语均并入该题单元格、以【】包裹；
    开放题独立列。
  - **按得分表**：仅计分题（单选、矩阵），分值为数字类型单元格；0 分（无法评价）与未作答留空；不含文本题与评语。
  - **所用时间**：前端计时（作答页渲染起、提交成功止、每次重开重置、无后台暂停），
    `duration_seconds` 后端原样存。
- **飞书推送** —— 一评价者一张飞书**模板卡片**（模板变量`name`=评价者姓名、
  `dynamic_evaluation_url`=追加`larkAppId=<该企业的身份应用>`的「我的评估」H5，不深链）。
  链接经飞书 AppLink（`applink.feishu.cn/client/web_url/open?mode=window`）包装，在飞书内置浏览器
  独立窗口打开，免登依赖飞书登录态（需飞书 7.5+）。员工端登录页据该`larkAppId`选用对应企业的
  身份应用换票。下文卡片配置缺失时一键发送与单份发布均拒绝发布。投递经
  **`linapro-lark-push`**：本插件只交出评价者工号与卡片构造函数，寻址（工号 → 企业级 `user_id`）、
  推送应用凭据与跨企业扇出都由 lark-push 负责，本插件不读任何飞书应用凭据。任一企业投递成功即视为
  已触达。发布按问卷判定：某份问卷的待评评价者全部送达，这份问卷才发布。先对全部评价者干跑寻址一次
  （不调飞书），有人不可达的问卷不发布，且只属于它的评价者不发卡；再只向干跑通过问卷的评价者正式投递
  （每人一张卡），投递后仍有人未送达的问卷不发布。单份发布失败即报错并按工号点名；一键发送部分发布，
  未发布的问卷连同原因与工号逐份提示。发布前的校验（空问卷、权重、推送配置）仍整批判定，任一不满足
  则整次拒绝。已送达的卡片不撤回，失败问卷重新发布时会再收到一张。跨企业双发可接受、不去重。
- **员工端作答（H5）** —— 待办列表、取题（按二维可见性过滤、直读 live 结构、无快照）、
  提交（时间窗 + 必填 + 去重校验，唯一索引幂等）。

## 二期（推迟）

整个报表族（题目得分表、主观评价展示、开放题展示、个人报告、总体平均分）、三层
聚合计分 + 角色权重加权 + 缺角色计 0 + 精度舍入，以及定时催办 cron。

## 依赖

- **`linapro-employee-core`** 经 `backend/cap/empcap`：员工身份
  （`EmpContext{EmpID, TenantID}`，经 `EmpAuth()` 注入）与员工查询（EmpID → 工号 / 姓名；按姓名录入评价关系时
  经 `MapByNamesAnyTenant` 姓名 → 员工）。
- **`linapro-lark-push`** 经 `backend/cap/larkpushcap`：推送应用清单（`ListPushApps`，发布前
  按应用预检模板）与卡片投递（`SendCard`），均以本插件 ID 为推送分组（`Group=linapro-eval-360`），
  测评卡片由独立的测评机器人发出。飞书凭据只在 lark-push 的
  `plugin.linapro-lark-push.push_groups.linapro-eval-360`，本插件不读取任何飞书应用配置。
- **`linapro-lark-sdk/larkim`**（`linapro-lark-sdk`）：仅用 `BuildTemplateCard` 构造模板卡片 JSON。
- 宿主 `hostconfigcap` 读本插件的卡片配置。

## 配置

宿主配置`plugin.linapro-eval-360`下：

| 配置键 | 必填 | 说明 |
|--------|------|------|
| `todo_h5_url` | 是 | 「我的评估」完整 URL，如`https://host/admin/#/eval-360/emp/todo`（hash 路由） |
| `card_templates` | 是 | 按**推送应用**分别配置的卡片模板数组，元素为 `{app_id, template_id, template_version}`，`app_id` 填 `plugin.linapro-lark-push.push_groups.linapro-eval-360` 里推送应用的 app_id（不是登录应用）。卡片模板须授权给实际发送的推送应用、跨企业不可复用，故**每个推送应用都必须有一条**，没有全局兜底；`template_version` 留空表示使用模板当前发布版本。缺任一推送应用的模板、或 lark-push 中本插件分组没有可用推送应用时，一键发送与单份发布都会在发布前拒绝 |

飞书凭据（推送应用）配置在 `plugin.linapro-lark-push.push_groups.linapro-eval-360`，见该插件
README；该分组未配置、或其中任一条目凭据为空（lark-push 判定该分组配置非法）都会拦截发布。
其它插件的分组与本插件互不影响。

各企业**身份应用**（`linapro-employee-core.lark_apps`）的重定向 URL 白名单须包含不带参数的员工登录页地址，如
`https://host/admin/#/eval-360/emp/login`（hash 路由）或
`https://host/admin/eval-360/emp/login`（history 路由）。

## 认证 / 多租户

- 管理端复用宿主 `sys_user` + `Auth()/Permission()` 链。
- 员工端复用 `linapro-employee-core` 的 `EmpAuth()` / `EmpContext`（员工平行认证，
  见该插件 README）。本插件为**平台级**（`scope_nature: platform_only`），自有
  `plugin_linapro_eval_360_*` 全表**不带 `tenant_id`**、不经 `ApplyPluginTableFilter`；
  租户仅在调用 `empcap` 解析工号时经 `EmpContext.TenantID` 进入。本插件不碰飞书
  OAuth。

## 本地调试员工端

员工端正常需经飞书免登拿员工 JWT。本地可用开发工具
[`hack/tools/emptoken`](../../../hack/tools/emptoken/README.zh-CN.md) 直接签发员工 JWT 扮演指定员工，
不改任何登录流程：

```bash
# 仓库根目录执行：按工号查本地员工表并校验在职，默认有效期 30 天
go run ./hack/tools/emptoken -emp-no GZ000071 [-ttl 720h] [-tenant-id 1]
# 已知 EmpID 时跳过查库直接签（在职校验交给后端 EmpAuth）
go run ./hack/tools/emptoken -emp-id 1907 -tenant-id 1
```

前提：

- 本地宿主已安装并启用 `linapro-employee-core`（员工身份能力由它提供，未启用时按 fail-closed 不可用）；
- 本地员工表 `plugin_linapro_employee_core_employee` 中有该工号的在职员工；
- 本地 `config.yaml` 配置了 `plugin.linapro-employee-core.emp_jwt_secret`，工具与后端用同一份密钥。

步骤：起后端（`:9120`）→ 起前端（vite `:5666`，或直接用后端内嵌页面）→ 运行工具 →
打开 `http://localhost:5666/admin/`（或 `http://localhost:9120/admin/`），把工具输出的脚本粘到
浏览器 Console。脚本写入 `linapro:emp-auth-token` 与 `linapro:emp-lark-app-id`（值为 `local`）后
跳转到仪表盘 `/eval-360/emp/dashboard`；「我的评估」在 `/eval-360/emp/todo`。

注意：Chrome 首次往 Console 粘贴时会拦截并提示，需先手动输入 `allow pasting` 回车再粘贴。
若接口返回 401，前端会尝试飞书静默重登并因 `larkAppId=local` 失败，重新签发并粘贴即可。

完整设计见 `openspec/changes/linapro-eval-360/`（proposal/design/spec/tasks）。
