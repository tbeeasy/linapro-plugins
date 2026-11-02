# linapro-eval-360

A source plugin for management-level **360-degree performance evaluation**.
It is built on the evaluation relationship (who evaluates whom) as its spine
and supports the project → questionnaire → evaluatee → relationship structure,
role weights with pre-publish validation, full Excel import with a relationship
editor, two-dimensional question visibility, one-click Lark (Feishu) push, and
two HR-facing detail exports (by answer / by score).

## Phase 1 scope (current)

- **Build questionnaire** — four question types only: single-choice,
  single-line text, multi-line text, matrix scale (multi-choice and rating are
  dropped).
- **Matrix rating-item comments** — each rating item (column) header carries a
  `⋮` action bar: delete column / allow comment / move left / move right (draft
  only; persisted with the question). "Allow comment" is a switch whose three
  sub-items can be combined: required (validated on submit only — a selected
  column must carry a non-empty comment), placeholder (the comment box's
  placeholder) and presets (one option per line plus whether custom input is
  allowed). On the employee side the comment box appears under the radio of the
  selected column; single-choice options get neither placeholder nor presets.
- **Edit strategies** — three strategies on a questionnaire with existing
  answers: ① keep answers (whitelist: change text / add question / add option /
  reorder only; reject changing score, deleting question/option/answer),
  ② clear answers (wipe all answers + reset relation completion, then edit
  freely), ③ copy questionnaire (deep-copy structure + evaluatee→relation tree,
  no answers; new copy is unpublished, not auto-pushed, original not locked).
- **Roles & weights** — no built-in roles; roles are generated from the
  import "role" column with default weight 0. Pre-publish validation requires
  each role weight > 0 and the sum = 100%; a role that should not count is
  simply not imported.
- **Relations** — unique key at the questionnaire level
  `(project_id, questionnaire_id, evaluatee_id, evaluator_emp_id)`.
  Full Excel import is the only bulk path and is allowed only before publish;
  after publish, additions go through the relationship editor add modal. Both
  take employee **names only** (no employee numbers): a name must match exactly
  one active employee across tenants; if a name is shared by two or more active
  employees (departed ones are ignored) or matches nobody, the submission is
  rejected and the offending names are listed. Import columns are recognized by
  header, so the export (which keeps the two employee-number columns for
  checking) can be re-imported as-is.
- **Publish / send** — questionnaire-level publish (unpublished = invisible to
  anyone); publishing a single questionnaire pushes cards to its evaluators.
  Project-level "send all" = push all unpublished questionnaires and publish them
  per questionnaire (published ones skipped, idempotent, never re-pushed). `end_at` is project-level, inherited by
  questionnaires. Voided status is derived (unanswered AND now > end_at), not
  persisted, no cron; the submit endpoint rejects on `now > end_at`.
- **Two detail exports** — two independent buttons (by answer / by score),
  each emits a single Excel (no ZIP), mounted at questionnaire level under the
  "Report" menu, exporting only submitted answers. Fixed lead columns:
  `序号 | 提交时间 | 所用时间 | 关系角色 | 被评价对象`.
  - **By-answer**: all question text; one column per matrix question (rows are
    evaluatees, cell = chosen item); option-level and matrix rating-item comments
    folded into the cell wrapped in `【】`; open questions in separate columns.
  - **By-score**: scoring questions only (single-choice, matrix), numeric-typed
    cells; 0-score ("unable to evaluate") and unanswered cells left blank;
    no text or comments.
  - **Duration**: front-end timed (render start → submit success, reset on
    each reopen, no server-side pause); `duration_seconds` stored verbatim.
- **Lark push** — one Feishu **template card** per evaluator (template variables
  `name` = evaluator name, `dynamic_evaluation_url` = "My Assessments" H5 with
  `larkAppId=<the enterprise's identity app>` appended; no deep link). The link is
  wrapped in a Feishu AppLink (`applink.feishu.cn/client/web_url/open?mode=window`)
  so it opens in a standalone window of Feishu's in-app browser, whose Feishu
  session makes login silent (Feishu 7.5+). The employee login page uses that
  `larkAppId` to pick the enterprise's identity app for the code exchange.
  One-click send and single-questionnaire publish both refuse to publish when the
  card configuration below is missing. Delivery goes through
  **`linapro-lark-push`**: this plugin only hands over evaluator employee numbers
  and a card builder; addressing (employee number → enterprise `user_id`), push-app
  credentials and cross-enterprise fan-out all belong to lark-push, and this plugin
  reads no Feishu app credentials. An evaluator counts as reached when any
  enterprise delivers. Publishing is decided per questionnaire: a questionnaire is
  published only when all of its pending evaluators are reached. One dry run (no
  Feishu calls) checks addressing for all evaluators; a questionnaire with an
  unreachable evaluator is not published, and evaluators who belong only to it get
  no card. Cards are then delivered only to evaluators of questionnaires that
  passed the dry run (one card per person), and a questionnaire with any evaluator
  still not reached is not published. Single-questionnaire publish fails with an
  error naming them by employee number; "send all" publishes what it can and lists
  each unpublished questionnaire with its reason and employee numbers. Pre-publish
  validation (empty questionnaires, weights, push configuration) is still all or
  nothing. Delivered cards are not recalled, so those evaluators get another card
  when a failed questionnaire is published again. Cross-enterprise double-send is
  accepted and not de-duplicated.
- **Employee answering (H5)** — todo list, question fetch (filtered by
  two-dimensional visibility, reading live structure — no snapshot), submit with
  time-window + required + dedup validation (unique-index idempotency).

## Phase 2 (deferred)

The entire report family (question-score table, subjective-comment display,
open-question display, personal report, overall average score), three-tier
aggregation + role-weight weighting + missing-role-zero + precision rounding,
and the scheduled reminder cron.

## Dependencies

- **`linapro-employee-core`** via `backend/cap/empcap`: employee identity
  (`EmpContext{EmpID, TenantID}` from `EmpAuth()`) and employee lookup
  (EmpID → employee number / name; name → employees via `MapByNamesAnyTenant`
  for name-based relation entry).
- **`linapro-lark-push`** via `backend/cap/larkpushcap`: the push-app list
  (`ListPushApps`, used for the per-app template precheck before publishing) and
  card delivery (`SendCard`), both with this plugin's ID as the push group
  (`Group=linapro-eval-360`), so assessment cards come from a dedicated bot.
  Feishu credentials live only in lark-push's
  `plugin.linapro-lark-push.push_groups.linapro-eval-360`; this plugin reads no
  Feishu app config.
- **`linapro-lark-sdk/larkim`** (`linapro-lark-sdk`): only `BuildTemplateCard`, to
  build the template card JSON.
- Host `hostconfigcap` for reading this plugin's card configuration.

## Configuration

Host config under `plugin.linapro-eval-360`:

| Key | Required | Description |
|-----|----------|-------------|
| `todo_h5_url` | Yes | Full "My Assessments" URL, e.g. `https://host/admin/#/eval-360/emp/todo` (hash router) |
| `card_templates` | Yes | Per-**push-app** card templates, an array of `{app_id, template_id, template_version}`, where `app_id` is a push app's app_id from `plugin.linapro-lark-push.push_groups.linapro-eval-360` (not the login app). A template must be granted to the push app that sends it and cannot be shared across enterprises, so **every push app needs its own entry**; there is no global fallback. Empty `template_version` = currently published version. If any push app has no template, or this plugin's group in lark-push has no usable push app, both "send all" and single-questionnaire publish are refused before publishing |

Feishu credentials (push apps) are configured under
`plugin.linapro-lark-push.push_groups.linapro-eval-360`; see that plugin's README.
An unconfigured group, or any entry in it with empty credentials (lark-push then
treats this group as invalid), blocks publishing. Other plugins' groups do not
affect this one.

Each enterprise's **identity app** (`linapro-employee-core.lark_apps`) redirect URL allowlist must contain the employee login page
URL without query, e.g. `https://host/admin/#/eval-360/emp/login` (hash router)
or `https://host/admin/eval-360/emp/login` (history router).

## Auth / multi-tenancy

- Admin side reuses the host `sys_user` + `Auth()/Permission()` chain.
- Employee side reuses `linapro-employee-core`'s `EmpAuth()` / `EmpContext`
  (parallel employee auth; see that plugin's README). This plugin is
  **platform-level** (`scope_nature: platform_only`): its own
  `plugin_linapro_eval_360_*` tables carry **no `tenant_id`** and are not routed
  through `ApplyPluginTableFilter`. A tenant enters only via `EmpContext.TenantID`
  when resolving employee numbers through `empcap`. The plugin does not touch
  Lark OAuth.

## Local employee-side debugging

The employee side normally obtains its JWT through Lark silent login. Locally,
the dev tool [`hack/tools/emptoken`](../../../hack/tools/emptoken/README.md)
signs an employee JWT directly so you can act as a given employee, without
changing any login flow:

```bash
# Run from the repo root: looks up the local employee table by employee number
# and checks the employee is active. Default TTL is 30 days.
go run ./hack/tools/emptoken -emp-no GZ000071 [-ttl 720h] [-tenant-id 1]
# Skip the lookup when the EmpID is known (EmpAuth still checks active status)
go run ./hack/tools/emptoken -emp-id 1907 -tenant-id 1
```

Prerequisites:

- `linapro-employee-core` is installed and enabled on the local host (it owns
  employee identity, which is fail-closed when the plugin is not enabled);
- the local employee table `plugin_linapro_employee_core_employee` contains an
  active employee with that number;
- local `config.yaml` sets `plugin.linapro-employee-core.emp_jwt_secret`; the
  tool signs with the same secret the backend verifies with.

Steps: start the backend (`:9120`) → start the frontend (vite `:5666`, or use
the backend-embedded pages) → run the tool → open `http://localhost:5666/admin/`
(or `http://localhost:9120/admin/`) and paste the printed script into the
browser console. It writes `linapro:emp-auth-token` and
`linapro:emp-lark-app-id` (value `local`), then redirects to the dashboard
`/eval-360/emp/dashboard`; "My Assessments" is `/eval-360/emp/todo`.

Note: Chrome blocks the first paste into the console; type `allow pasting`
manually and press Enter, then paste again. On a 401 the frontend attempts Lark silent re-login, which fails with
`larkAppId=local`; just mint and paste a new token.

See `openspec/changes/linapro-eval-360/` for the full design (proposal/design/
spec/tasks).
