-- linapro-eval-360: 删除 question_option / matrix_column 的 is_na 列
--
-- 背景：前端录入侧（单选 + 矩阵）已移除「无法评价」特殊项，is_na 恒为 0，
-- 后端 is_na 概念已成死代码。本迁移把两表的 is_na 列彻底删除。
-- 删除后 N/A 仅保留「未作答 / 未命中」语义（by-score 导出未答格仍显 N/A，
-- 由取分逻辑承载，与 DB 列无关）。
-- 用 IF EXISTS，重复执行安全。

ALTER TABLE plugin_linapro_eval_360_question_option
    DROP COLUMN IF EXISTS "is_na";

ALTER TABLE plugin_linapro_eval_360_matrix_column
    DROP COLUMN IF EXISTS "is_na";
