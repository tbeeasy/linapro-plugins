-- linapro-eval-360: 题目新增「描述」列（富文本 HTML，可空）
--
-- 背景：题目标题改用富文本编辑器，并新增一个同样富文本的「描述」字段。
-- 标题沿用已有 title TEXT 列（可存 HTML），本迁移仅为存量库补 description 列。
-- 用 IF NOT EXISTS，重复执行安全；不改动已有数据。

ALTER TABLE plugin_linapro_eval_360_question
    ADD COLUMN IF NOT EXISTS "description" TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN plugin_linapro_eval_360_question."description" IS '题目描述（富文本 HTML，可空）';
