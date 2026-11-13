-- linapro-eval-360: 被评估人新增「项目」列（录入文本，可空）
--
-- 背景：评价关系的全量导入、新增弹窗、明细列表与导出需携带被评估人所在的业务项目。
-- 与职务/部门同口径：挂在被评估人（问卷级）上，由管理员录入，不从员工主数据派生。
-- 列名取 work_project 而非 project_name：本表已有 project_id 指测评项目，避免两者混淆。
-- 用 IF NOT EXISTS，重复执行安全；存量行取默认空串，不改动已有数据。

ALTER TABLE plugin_linapro_eval_360_evaluatee
    ADD COLUMN IF NOT EXISTS "work_project" VARCHAR(200) NOT NULL DEFAULT '';
COMMENT ON COLUMN plugin_linapro_eval_360_evaluatee."work_project" IS '被评估人所在项目（导入/新增弹窗录入，可空；与测评项目 project_id 无关）';
