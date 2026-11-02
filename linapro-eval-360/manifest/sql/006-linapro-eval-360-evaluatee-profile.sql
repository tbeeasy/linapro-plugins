-- linapro-eval-360: 被评估人新增「职务」「部门」两列（录入文本，可空）
--
-- 背景：评价关系的全量导入、新增弹窗、明细列表与导出均需携带被评估人职务与部门。
-- 两者挂在被评估人（问卷级）上，由管理员录入，不从员工主数据派生
-- （empcap 无职务字段，部门仅为飞书部门 ID）。
-- 用 IF NOT EXISTS，重复执行安全；存量行取默认空串，不改动已有数据。

ALTER TABLE plugin_linapro_eval_360_evaluatee
    ADD COLUMN IF NOT EXISTS "job_title" VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_eval_360_evaluatee
    ADD COLUMN IF NOT EXISTS "department" VARCHAR(200) NOT NULL DEFAULT '';
COMMENT ON COLUMN plugin_linapro_eval_360_evaluatee."job_title"  IS '被评估人职务（导入/新增弹窗录入，可空）';
COMMENT ON COLUMN plugin_linapro_eval_360_evaluatee."department" IS '被评估人部门（导入/新增弹窗录入，可空）';
