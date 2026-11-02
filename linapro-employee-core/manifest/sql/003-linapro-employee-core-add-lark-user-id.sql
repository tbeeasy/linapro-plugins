-- ------------------------------------------------------------
-- 003 linapro-employee-core 迁移：lark_identity 增加 user_id 富化列
-- 用途：为 lark_identity 增加 lark_user_id 列，支撑考勤等只按 user_id 推送
-- （不带 open_id）的飞书数据源反查内部 emp_id。user_id 是企业（租户）级标识，
-- 跨该企业所有 app 一致；同企业配多个 app 时同人各行值相同（冗余但均指向同一
-- emp_id，反查取任意一行即可）。回填由现有全量同步在枚举通讯录时顺带写入，
-- 不新增 API 调用与同步任务；应用无 user_id 权限时该列为空。
-- ------------------------------------------------------------

ALTER TABLE plugin_linapro_employee_core_lark_identity
    ADD COLUMN IF NOT EXISTS "lark_user_id" VARCHAR(128) NOT NULL DEFAULT '';

COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."lark_user_id"
    IS '飞书 user_id（企业级，跨该企业所有 app 一致）；考勤等按 user_id 推送的数据源用它反查 emp_id；应用无 user_id 权限时为空';

-- 反查索引：考勤事件按 (lark_app_id, lark_user_id) 双键解析 emp_id，
-- larkAppID 定位企业以避免跨企业 user_id 碰撞。仅对非空 user_id 建索引。
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_employee_core_lark_identity_user
    ON plugin_linapro_employee_core_lark_identity ("lark_app_id", "lark_user_id")
    WHERE "lark_user_id" <> '';
