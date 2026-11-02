-- ------------------------------------------------------------
-- 004 linapro-employee-core 迁移：lark_identity 增加 id_source 列
-- 用途：区分本租户原生身份（own，contact.User.List/FindByDepartment）与
-- 关联组织视角身份（assoc，directory.collaboration_share_entity.list）。
-- assoc 行仅为写多维表格人员字段采集，MUST NOT 成为登录/考勤凭据：
-- 登录 ResolveEmpIDByLarkIdentity 与考勤反查 ResolveEmpIDByLarkUserID 一律
-- 加 id_source='own' 过滤，保证引入 assoc 行后登录仍失败关闭（fail-closed）。
-- 主键 (lark_app_id, lark_open_id) 不变；同一自然人跨飞书企业或同时为 own/assoc
-- 时各写一行（最多 4 行：两 app × own/assoc），均指向同一 emp_id。
-- ------------------------------------------------------------

ALTER TABLE plugin_linapro_employee_core_lark_identity
    ADD COLUMN IF NOT EXISTS "id_source" VARCHAR(16) NOT NULL DEFAULT 'own';

COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."id_source"
    IS '身份来源：own=本租户原生身份（可登录/考勤反查）；assoc=关联组织视角身份（仅供写多维表格人员字段，不参与登录/反查）';
