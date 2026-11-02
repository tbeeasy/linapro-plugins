-- linapro-eval-360: 360 度绩效测评表结构
--
-- 脊柱: project(1) → questionnaire(N) → evaluatee(N) → relation(N)
-- 本插件为平台级（platform_only）：问卷对所有租户员工通用，不做租户隔离，故全表不带 tenant_id。
-- 计分列使用 NUMERIC，无任何 float 列。
-- 无快照表：取题、计分、导出一律直读 live 结构。
-- 唯一键下沉到问卷级（含 questionnaire_id），保证策略③复制问卷后两棵关系树互不冲突。

-- ============================================================================
-- 1. 测评项目 project：一次评估活动的顶层容器
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_project (
    "id"           BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "name"         VARCHAR(255) NOT NULL DEFAULT '',
    "description"  TEXT         NOT NULL DEFAULT '',
    "start_at"     TIMESTAMPTZ,
    "end_at"       TIMESTAMPTZ,                       -- 项目级统一截止；到点后拒绝提交
    "closed_at"    TIMESTAMPTZ,                       -- 提前结束时间戳，可空可清空（可逆重开），不改 end_at
    "created_by"   INT          NOT NULL DEFAULT 0,   -- 创建管理员 sys_user.id
    "created_at"   TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"   TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_project IS '360 测评项目：一次评估活动的顶层容器';
COMMENT ON COLUMN plugin_linapro_eval_360_project."name"        IS '项目名称';
COMMENT ON COLUMN plugin_linapro_eval_360_project."description" IS '项目描述';
COMMENT ON COLUMN plugin_linapro_eval_360_project."start_at"    IS '评估开始时间';
COMMENT ON COLUMN plugin_linapro_eval_360_project."end_at"      IS '项目统一截止时间；超过后拒绝提交';
COMMENT ON COLUMN plugin_linapro_eval_360_project."closed_at"    IS '提前结束时间戳，空=未提前结束；清空即重新开启；不改 end_at';
COMMENT ON COLUMN plugin_linapro_eval_360_project."created_by"  IS '创建管理员 sys_user.id';
-- 项目名全平台唯一（归一化后精确匹配；见 002-linapro-eval-360-name-unique.sql）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_project_name
    ON plugin_linapro_eval_360_project ("name");

-- ============================================================================
-- 2. 问卷 questionnaire：项目下的测评表，含发布状态
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_questionnaire (
    "id"           BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "project_id"   BIGINT       NOT NULL,
    "title"        VARCHAR(255) NOT NULL DEFAULT '',
    "description"  TEXT         NOT NULL DEFAULT '',
    "published"    SMALLINT     NOT NULL DEFAULT 0,    -- 0=未发布（评价者不可见） 1=已发布
    "sort"         INT          NOT NULL DEFAULT 0,
    "created_at"   TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"   TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
    -- 问卷不设 end_at：截止统一取项目 end_at
);
COMMENT ON TABLE  plugin_linapro_eval_360_questionnaire IS '360 测评问卷：项目下的测评表，含问卷级发布状态';
COMMENT ON COLUMN plugin_linapro_eval_360_questionnaire."project_id" IS '所属项目 id';
COMMENT ON COLUMN plugin_linapro_eval_360_questionnaire."published"  IS '发布状态：0=未发布（不进评价者待办） 1=已发布';
COMMENT ON COLUMN plugin_linapro_eval_360_questionnaire."sort"       IS '问卷排序';
CREATE INDEX IF NOT EXISTS idx_eval_360_questionnaire_project
    ON plugin_linapro_eval_360_questionnaire ("project_id");
-- 问卷标题同一项目内唯一，不同项目可重名（见 002-linapro-eval-360-name-unique.sql）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_questionnaire_project_title
    ON plugin_linapro_eval_360_questionnaire ("project_id", "title");

-- ============================================================================
-- 3. 维度 dimension：问卷下题目的分组
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_dimension (
    "id"              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "questionnaire_id" BIGINT      NOT NULL,
    "name"            VARCHAR(255) NOT NULL DEFAULT '',
    "sort"            INT          NOT NULL DEFAULT 0,
    "created_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_dimension IS '360 测评维度：问卷下题目的分组';
COMMENT ON COLUMN plugin_linapro_eval_360_dimension."questionnaire_id" IS '所属问卷 id';
COMMENT ON COLUMN plugin_linapro_eval_360_dimension."name"            IS '维度名称';
COMMENT ON COLUMN plugin_linapro_eval_360_dimension."sort"            IS '维度排序';
CREATE INDEX IF NOT EXISTS idx_eval_360_dimension_q
    ON plugin_linapro_eval_360_dimension ("questionnaire_id");

-- ============================================================================
-- 4. 题目 question：单选/单行文本/多行文本/矩阵量表（不含多选、评分）
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_question (
    "id"              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "questionnaire_id" BIGINT      NOT NULL,
    "dimension_id"    BIGINT       NOT NULL,            -- 每题必须挂维度
    "type"            VARCHAR(20)  NOT NULL,           -- single_choice|text_short|text_long|matrix
    "title"           TEXT         NOT NULL DEFAULT '', -- 题目标题（富文本 HTML）
    "description"     TEXT         NOT NULL DEFAULT '', -- 题目描述（富文本 HTML，可空）
    "required"        SMALLINT     NOT NULL DEFAULT 0,  -- 是否必填（提交时按过滤后 live 结构校验）
    "sort"            INT          NOT NULL DEFAULT 0,
    "created_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_question IS '360 测评题目：单选/单行文本/多行文本/矩阵量表';
COMMENT ON COLUMN plugin_linapro_eval_360_question."questionnaire_id" IS '所属问卷 id';
COMMENT ON COLUMN plugin_linapro_eval_360_question."dimension_id"    IS '所属维度 id（每题必须挂维度）';
COMMENT ON COLUMN plugin_linapro_eval_360_question."type"            IS '题型：single_choice|text_short|text_long|matrix';
COMMENT ON COLUMN plugin_linapro_eval_360_question."title"           IS '题目标题（富文本 HTML）';
-- description 的列注释由 003 迁移负责（原因同被评估人 job_title：保留数据重装时旧表不含该列）。
COMMENT ON COLUMN plugin_linapro_eval_360_question."required"        IS '是否必填：0=否 1=是';
COMMENT ON COLUMN plugin_linapro_eval_360_question."sort"            IS '题目排序';
CREATE INDEX IF NOT EXISTS idx_eval_360_question_q
    ON plugin_linapro_eval_360_question ("questionnaire_id");

-- ============================================================================
-- 5. 题目可见性 question_visibility：二维 AND（visible_roles ∩ 受限评价者排除）
--    每题可有可无；一张表承载两个可独立为空的约束。
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_question_visibility (
    "id"               BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "question_id"      BIGINT      NOT NULL,
    "visible_role_ids" TEXT        NOT NULL DEFAULT '',  -- 角色 id 逗号分隔；空=全员可见
    "created_at"       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_question_visibility IS '360 测评题目可见性：visible_roles（空=全员）与受限评价者名单（排除语义）';
COMMENT ON COLUMN plugin_linapro_eval_360_question_visibility."question_id"     IS '所属题目 id';
COMMENT ON COLUMN plugin_linapro_eval_360_question_visibility."visible_role_ids" IS '可见角色 id 逗号分隔，空=所有角色可见';
CREATE INDEX IF NOT EXISTS idx_eval_360_qvis_question
    ON plugin_linapro_eval_360_question_visibility ("question_id");

-- 受限评价者名单（排除语义）：名单内的评价者本人在该问卷中看不到该题，
-- 与其评谁无关（全局生效）；同问卷其他评价者不受影响。
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_question_excluded_evaluator (
    "id"               BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "question_id"      BIGINT      NOT NULL,
    "evaluator_emp_id" BIGINT      NOT NULL,               -- 被排除的评价者 EmpID（评价者本人身份）
    "created_at"       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_question_excluded_evaluator IS '360 测评题目受限评价者名单：名单内的评价者看不到该题（排除语义，按评价者本人全局生效）';
COMMENT ON COLUMN plugin_linapro_eval_360_question_excluded_evaluator."question_id"      IS '所属题目 id';
COMMENT ON COLUMN plugin_linapro_eval_360_question_excluded_evaluator."evaluator_emp_id" IS '被排除的评价者 EmpID';
CREATE INDEX IF NOT EXISTS idx_eval_360_qexc_question
    ON plugin_linapro_eval_360_question_excluded_evaluator ("question_id");

-- ============================================================================
-- 6. 单选选项 question_option：score / allow_text
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_question_option (
    "id"          BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "question_id" BIGINT       NOT NULL,
    "label"       VARCHAR(255) NOT NULL DEFAULT '',
    "score"       NUMERIC(10,2) NOT NULL DEFAULT 0,    -- 选项分值
    "allow_text"  SMALLINT     NOT NULL DEFAULT 0,    -- 1=允许附文本评语（随答案保存、并入单元格【】包裹）
    "sort"        INT          NOT NULL DEFAULT 0,
    "created_at"  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_question_option IS '360 测评单选选项：score/allow_text';
COMMENT ON COLUMN plugin_linapro_eval_360_question_option."question_id" IS '所属单选题 id';
COMMENT ON COLUMN plugin_linapro_eval_360_question_option."label"      IS '选项文案';
COMMENT ON COLUMN plugin_linapro_eval_360_question_option."score"       IS '选项分值，NUMERIC';
COMMENT ON COLUMN plugin_linapro_eval_360_question_option."allow_text"  IS '是否允许附文本评语：1=是';
COMMENT ON COLUMN plugin_linapro_eval_360_question_option."sort"        IS '选项排序';
CREATE INDEX IF NOT EXISTS idx_eval_360_option_question
    ON plugin_linapro_eval_360_question_option ("question_id");

-- ============================================================================
-- 7. 矩阵题行 matrix_row 与列选项 matrix_column（列含分值）
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_matrix_row (
    "id"          BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "question_id" BIGINT       NOT NULL,
    "title"       TEXT         NOT NULL DEFAULT '',
    "sort"        INT          NOT NULL DEFAULT 0,
    "created_at"  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_matrix_row IS '360 测评矩阵题行标题';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_row."question_id"  IS '所属矩阵题 id';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_row."title"       IS '行标题';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_row."sort"         IS '行排序';
CREATE INDEX IF NOT EXISTS idx_eval_360_mrow_question
    ON plugin_linapro_eval_360_matrix_row ("question_id");

CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_matrix_column (
    "id"          BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "question_id" BIGINT       NOT NULL,
    "label"       VARCHAR(255) NOT NULL DEFAULT '',
    "score"       NUMERIC(10,2) NOT NULL DEFAULT 0,
    "sort"        INT          NOT NULL DEFAULT 0,
    "allow_comment"        INT          NOT NULL DEFAULT 0,   -- 是否允许附评语
    "comment_required"     INT          NOT NULL DEFAULT 0,   -- 评语是否必填（提交时校验）
    "comment_placeholder"  VARCHAR(255) NOT NULL DEFAULT '',  -- 评语输入框占位提示
    "comment_presets"      TEXT         NOT NULL DEFAULT '',  -- 预设填空候选（JSON 数组文本）
    "comment_allow_custom" INT          NOT NULL DEFAULT 0,   -- 有预设时是否允许自定义输入
    "created_at"  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_matrix_column IS '360 测评矩阵题列选项';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."question_id"  IS '所属矩阵题 id';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."label"       IS '列文案';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."score"       IS '列分值，NUMERIC';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."sort"         IS '列排序';
-- allow_comment 等五个评语列的列注释由 007 迁移负责（原因同被评估人 job_title：保留数据重装时旧表不含该列）。
CREATE INDEX IF NOT EXISTS idx_eval_360_mcol_question
    ON plugin_linapro_eval_360_matrix_column ("question_id");

-- ============================================================================
-- 8. 角色 role：每问卷一个实体，由导入生成，默认权重 0（无内置角色）
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_role (
    "id"              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "questionnaire_id" BIGINT      NOT NULL,
    "name"            VARCHAR(255) NOT NULL DEFAULT '',  -- 归一化后的名称（去首尾空白+折叠内部空白+全角转半角）
    "weight"          NUMERIC(5,2) NOT NULL DEFAULT 0,   -- 权重；发布前每角色 >0 且和=100%
    "created_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_role IS '360 测评角色：每问卷一个实体，由导入生成，默认权重 0';
COMMENT ON COLUMN plugin_linapro_eval_360_role."questionnaire_id" IS '所属问卷 id';
COMMENT ON COLUMN plugin_linapro_eval_360_role."name"            IS '角色名（归一化后精确匹配）';
COMMENT ON COLUMN plugin_linapro_eval_360_role."weight"          IS '角色权重，NUMERIC；发布前每角色 >0 且和=100%';
CREATE INDEX IF NOT EXISTS idx_eval_360_role_q
    ON plugin_linapro_eval_360_role ("questionnaire_id");
-- 角色名问卷内唯一（归一化后）
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_role_q_name
    ON plugin_linapro_eval_360_role ("questionnaire_id", "name");

-- ============================================================================
-- 9. 被评估人 evaluatee：问卷下的被评估员工，引用 empcap EmpID
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_evaluatee (
    "id"              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "project_id"      BIGINT       NOT NULL,
    "questionnaire_id" BIGINT      NOT NULL,
    "evaluatee_emp_id" BIGINT      NOT NULL,            -- empcap EmpID
    "job_title"       VARCHAR(100) NOT NULL DEFAULT '',  -- 被评估人职务（录入文本，可空）
    "department"      VARCHAR(200) NOT NULL DEFAULT '',  -- 被评估人部门（录入文本，可空）
    "created_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_evaluatee IS '360 测评被评估人：问卷下的被评估员工';
COMMENT ON COLUMN plugin_linapro_eval_360_evaluatee."project_id"       IS '所属项目 id（冗余便于查询）';
COMMENT ON COLUMN plugin_linapro_eval_360_evaluatee."questionnaire_id"  IS '所属问卷 id';
COMMENT ON COLUMN plugin_linapro_eval_360_evaluatee."evaluatee_emp_id"  IS '被评估员工 EmpID（linapro-employee-core）';
-- job_title / department 的列注释由 006 迁移负责：卸载保留数据后重装时旧表仍在、
-- 上方 CREATE TABLE IF NOT EXISTS 不生效，此处注释后加列会因列不存在而中断安装。
CREATE INDEX IF NOT EXISTS idx_eval_360_evaluatee_q
    ON plugin_linapro_eval_360_evaluatee ("questionnaire_id");
-- 被评估人唯一键下沉到问卷级：红线 questionnaire_id 必须在键内
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_evaluatee_q_emp
    ON plugin_linapro_eval_360_evaluatee ("project_id", "questionnaire_id", "evaluatee_emp_id");

-- ============================================================================
-- 10. 评价关系 relation：(被评估人, 评价者) 唯一，带角色属性与完成状态
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_relation (
    "id"               BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "project_id"       BIGINT       NOT NULL,
    "questionnaire_id"  BIGINT      NOT NULL,
    "evaluatee_id"     BIGINT       NOT NULL,           -- evaluatee.id
    "evaluator_emp_id"  BIGINT      NOT NULL,           -- 评价者 empcap EmpID
    "role_id"          BIGINT       NOT NULL,           -- 角色 role.id（属性，不进唯一键）
    "source"           VARCHAR(20)  NOT NULL DEFAULT 'import', -- import|manual
    "completed"        SMALLINT     NOT NULL DEFAULT 0, -- 0=未填 1=已填（派生已作废不落库）
    "completed_at"     TIMESTAMPTZ,
    "created_at"       TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at"       TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_relation IS '360 测评评价关系：(被评估人, 评价者) 唯一，带角色属性与完成状态';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."project_id"       IS '所属项目 id';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."questionnaire_id"  IS '所属问卷 id（必须在唯一键内）';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."evaluatee_id"     IS '被评估人 evaluatee.id';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."evaluator_emp_id"  IS '评价者 EmpID';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."role_id"          IS '角色 role.id（属性，不进唯一键）';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."source"           IS '来源：import=全量导入 manual=关系编辑器手动新增';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."completed"        IS '完成状态：0=未填 1=已填；派生已作废不落库';
COMMENT ON COLUMN plugin_linapro_eval_360_relation."completed_at"     IS '首次提交时间';
CREATE INDEX IF NOT EXISTS idx_eval_360_relation_q_evaluator
    ON plugin_linapro_eval_360_relation ("questionnaire_id", "evaluator_emp_id");
CREATE INDEX IF NOT EXISTS idx_eval_360_relation_q_evaluatee
    ON plugin_linapro_eval_360_relation ("questionnaire_id", "evaluatee_id");
-- 评价关系唯一键下沉到问卷级：红线 questionnaire_id 必须在键内
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_relation_q_evaluator
    ON plugin_linapro_eval_360_relation ("project_id", "questionnaire_id", "evaluatee_id", "evaluator_emp_id");

-- ============================================================================
-- 11. 作答 submission：一条评价关系的首次提交（幂等，唯一索引兜底）
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_submission (
    "id"                BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "relation_id"       BIGINT       NOT NULL,
    "questionnaire_id"  BIGINT      NOT NULL,
    "evaluatee_id"      BIGINT       NOT NULL,
    "evaluator_emp_id"  BIGINT      NOT NULL,
    "duration_seconds"  INT          NOT NULL DEFAULT 0,  -- 前端计时（渲染起、提交止）上传的整数秒，后端原样存
    "submitted_at"      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_submission IS '360 测评作答：一条评价关系的首次提交（幂等）';
COMMENT ON COLUMN plugin_linapro_eval_360_submission."relation_id"       IS '评价关系 id';
COMMENT ON COLUMN plugin_linapro_eval_360_submission."questionnaire_id"  IS '所属问卷 id';
COMMENT ON COLUMN plugin_linapro_eval_360_submission."evaluatee_id"      IS '被评估人 id';
COMMENT ON COLUMN plugin_linapro_eval_360_submission."evaluator_emp_id"  IS '评价者 EmpID';
COMMENT ON COLUMN plugin_linapro_eval_360_submission."duration_seconds"  IS '作答时长（前端计时上传，后端原样存）';
COMMENT ON COLUMN plugin_linapro_eval_360_submission."submitted_at"      IS '提交时间';
CREATE INDEX IF NOT EXISTS idx_eval_360_submission_q
    ON plugin_linapro_eval_360_submission ("questionnaire_id");
-- 幂等兜底：同一评价关系只能提交一次
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_submission_relation
    ON plugin_linapro_eval_360_submission ("relation_id");

-- ============================================================================
-- 12. 答案 answer：一题一行（矩阵按行拆行），规范化存储，选项级评语随答案保存
-- ============================================================================
CREATE TABLE IF NOT EXISTS plugin_linapro_eval_360_answer (
    "id"               BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "submission_id"    BIGINT       NOT NULL,
    "questionnaire_id" BIGINT       NOT NULL,
    "question_id"      BIGINT       NOT NULL,
    "row_id"           BIGINT       NOT NULL DEFAULT 0,  -- 矩阵题：matrix_row.id；非矩阵为 0
    "option_id"        BIGINT       NOT NULL DEFAULT 0, -- 单选：question_option.id；矩阵：matrix_column.id；文本为 0
    "text_value"       TEXT         NOT NULL DEFAULT '', -- 文本题答案；或选项级评语（allow_text）
    "created_at"       TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE  plugin_linapro_eval_360_answer IS '360 测评答案：一题一行（矩阵按行拆行），选项级评语随答案保存';
COMMENT ON COLUMN plugin_linapro_eval_360_answer."submission_id"   IS '所属作答 id';
COMMENT ON COLUMN plugin_linapro_eval_360_answer."questionnaire_id" IS '所属问卷 id';
COMMENT ON COLUMN plugin_linapro_eval_360_answer."question_id"      IS '所属题目 id';
COMMENT ON COLUMN plugin_linapro_eval_360_answer."row_id"          IS '矩阵题行 id，非矩阵为 0';
COMMENT ON COLUMN plugin_linapro_eval_360_answer."option_id"       IS '单选选项 id / 矩阵列 id / 文本为 0';
COMMENT ON COLUMN plugin_linapro_eval_360_answer."text_value"      IS '文本题答案 或 选项级评语（allow_text）';
CREATE INDEX IF NOT EXISTS idx_eval_360_answer_submission
    ON plugin_linapro_eval_360_answer ("submission_id");
CREATE INDEX IF NOT EXISTS idx_eval_360_answer_question
    ON plugin_linapro_eval_360_answer ("question_id");
