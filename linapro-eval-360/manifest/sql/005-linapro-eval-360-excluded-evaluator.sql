-- linapro-eval-360: 题目受限名单由「受限被评估人」改为「受限评价者·全局」（FB-10）
--
-- 背景：二维题目可见性第 2 维的语义反转——受限名单装评价者本人身份（evaluator_emp_id），
-- 名单内的评价者在该问卷里看不到该题，与其评谁无关。旧表存的是被评估人行 id
-- （evaluatee_id），语义无法换算为评价者，故不做数据迁移：直接删除旧表、创建新表。
-- 001 基础 schema 已同步为新表，供全新建库；本文件负责已安装库的升级。
--
-- 顺序约束：先删旧表（其索引 idx_eval_360_qexc_question 随表删除），再建新表与同名索引，
-- 否则 CREATE INDEX IF NOT EXISTS 会因旧索引同名而被跳过。
-- 全部语句带存在性保护，重复执行结果一致。

DROP TABLE IF EXISTS plugin_linapro_eval_360_question_excluded_evaluatee;

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
