-- linapro-eval-360: 矩阵评价项（列）新增评语配置五列（FB-26）
--
-- 背景：矩阵题每个评价项可独立配置评语：是否允许评语、是否必填、
-- 输入框占位提示（提示语）、预设填空候选、除预设外是否允许自定义输入。
-- 提示语与预设填空互相独立可同时生效；评语必填只在提交时校验。
--
-- comment_presets 以 JSON 数组文本存放（如 ["沟通清晰","逻辑严谨"]），
-- 使用 TEXT 而非 JSONB：本插件的 DAO 生成只映射 numeric/timestamp，
-- JSONB 需额外 typeMapping 且 DO 层写入需自行序列化，TEXT 更直接可控。
-- 全部带默认值，存量行取「不允许评语」的空配置，不改动已有数据。

ALTER TABLE plugin_linapro_eval_360_matrix_column
    ADD COLUMN IF NOT EXISTS "allow_comment"         INT          NOT NULL DEFAULT 0;
ALTER TABLE plugin_linapro_eval_360_matrix_column
    ADD COLUMN IF NOT EXISTS "comment_required"      INT          NOT NULL DEFAULT 0;
ALTER TABLE plugin_linapro_eval_360_matrix_column
    ADD COLUMN IF NOT EXISTS "comment_placeholder"   VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_eval_360_matrix_column
    ADD COLUMN IF NOT EXISTS "comment_presets"       TEXT         NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_eval_360_matrix_column
    ADD COLUMN IF NOT EXISTS "comment_allow_custom"  INT          NOT NULL DEFAULT 0;

COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."allow_comment"        IS '是否允许附评语：0=否 1=是';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."comment_required"     IS '评语是否必填：0=否 1=是（仅允许评语时生效，提交时校验）';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."comment_placeholder"  IS '评语输入框占位提示（提示语），可空';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."comment_presets"      IS '预设填空候选，JSON 数组文本；空=无预设（自由输入）';
COMMENT ON COLUMN plugin_linapro_eval_360_matrix_column."comment_allow_custom" IS '是否允许自定义输入：0=只能选预设 1=可自定义（仅在有预设时生效）';
