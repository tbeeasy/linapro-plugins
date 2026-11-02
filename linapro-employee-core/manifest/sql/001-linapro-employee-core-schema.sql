-- ------------------------------------------------------------
-- 001 linapro-employee-core 架构 SQL 文件
-- 用途：插件私有的员工身份、部门归属与飞书身份映射。员工是区别于 sys_user 的租户内
-- 主体；业务数据引用内部 EmpID(id)，绝不引用 Lark open_id。同一自然人横跨两个飞书
-- 企业时只有一条 employee 行、两条 lark_identity 行（每个 lark_app_id 一条）指向同一
-- emp_id，考勤/问卷等业务数据不会分裂到两个人名下。
-- 表名约定：plugin_<plugin-id 下划线化>_<实体>
-- 凭证说明：飞书 App Secret 不落库，改从本插件私有配置
-- plugin.linapro-employee-core.lark_apps（飞书应用数组）读取，该配置已被
-- .gitignore 忽略、不提交版本库。
-- ------------------------------------------------------------

CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_employee (
    "id"               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "tenant_id"        INT NOT NULL DEFAULT 0,
    "name"             VARCHAR(191) NOT NULL DEFAULT '',
    "moka_employee_no" VARCHAR(64)  NOT NULL DEFAULT '',
    "status"           SMALLINT NOT NULL DEFAULT 1,
    "sys_user_id"      INT NOT NULL DEFAULT 0,
    "created_at"       TIMESTAMPTZ,
    "updated_at"       TIMESTAMPTZ,
    "deleted_at"       TIMESTAMPTZ
);

COMMENT ON TABLE plugin_linapro_employee_core_employee IS '租户内员工身份表，归 linapro-employee-core 管理；id 为内部 EmpID，业务数据引用此值；飞书身份映射见 lark_identity 表';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."id" IS '内部 EmpID：与身份提供方无关的代理主键，所有业务数据引用此值';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."tenant_id" IS '所属租户；同一自然人在两个租户中是两条记录';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."name" IS '员工姓名；Moka 为权威来源，登录时不覆盖';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."moka_employee_no" IS 'Moka HR 系统工号（如 GZ001198）；同租户内非空值唯一，是员工行存在的稳定锚点；未接入 Moka 时为空字符串';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."status" IS '在职状态：1=在职，0=离职；EmpAuth 每次请求查询此字段以实时拦截离职员工';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."sys_user_id" IS '可选关联：该员工同时是后台管理员时回填对应 sys_user.id；否则为 0';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."created_at" IS '创建时间';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."updated_at" IS '更新时间';
COMMENT ON COLUMN plugin_linapro_employee_core_employee."deleted_at" IS '软删除时间；存活行为 NULL';

-- 去重主键：同租户内工号非空存活行唯一。
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_linapro_employee_core_emp_tenant_moka_no
    ON plugin_linapro_employee_core_employee ("tenant_id", "moka_employee_no")
    WHERE "deleted_at" IS NULL AND "moka_employee_no" <> '';

CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_employee_dept (
    "id"                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "tenant_id"          INT NOT NULL DEFAULT 0,
    "emp_id"             BIGINT NOT NULL,
    "lark_department_id" VARCHAR(191) NOT NULL,
    "is_primary"         SMALLINT NOT NULL DEFAULT 0,
    "created_at"         TIMESTAMPTZ
);

COMMENT ON TABLE plugin_linapro_employee_core_employee_dept IS '员工部门归属表（存飞书原始部门 ID），归 linapro-employee-core 管理；与 org-core 部门树的映射由独立同步功能负责';
COMMENT ON COLUMN plugin_linapro_employee_core_employee_dept."emp_id" IS '所属员工的 EmpID';
COMMENT ON COLUMN plugin_linapro_employee_core_employee_dept."lark_department_id" IS '登录时从飞书采集的原始部门 ID';
COMMENT ON COLUMN plugin_linapro_employee_core_employee_dept."is_primary" IS '主部门标识：1=主部门，0=兼属部门';

CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_linapro_employee_core_dept_emp_dept
    ON plugin_linapro_employee_core_employee_dept ("emp_id", "lark_department_id");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_employee_core_dept_tenant_emp
    ON plugin_linapro_employee_core_employee_dept ("tenant_id", "emp_id");

-- 飞书身份映射：每个 (lark_app_id, lark_open_id) 一条，映射到 emp_id。同一员工横跨
-- 飞书企业 F 与 G 时有两条（每个 app 一条），都指向同一 emp_id。登录按主键解析 emp_id；
-- emp_id 索引支持反查与清理。
CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_lark_identity (
    "lark_app_id"  VARCHAR(128) NOT NULL DEFAULT '',
    "lark_open_id" VARCHAR(191) NOT NULL DEFAULT '',
    "lark_user_id" VARCHAR(128) NOT NULL DEFAULT '',
    "id_source"    VARCHAR(16)  NOT NULL DEFAULT 'own',
    "emp_id"       BIGINT NOT NULL,
    "created_at"   TIMESTAMPTZ,
    PRIMARY KEY ("lark_app_id", "lark_open_id")
);

COMMENT ON TABLE plugin_linapro_employee_core_lark_identity IS '飞书身份映射表：(lark_app_id, lark_open_id) → emp_id，归 linapro-employee-core 管理；解决同一自然人跨多个飞书企业各有一个 open_id 的问题';
COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."lark_app_id" IS '飞书 App ID（cli_xxx）；标识该 open_id 所属的飞书企业';
COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."lark_open_id" IS '飞书应用内 open_id；同一 lark_app_id 下唯一';
COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."lark_user_id" IS '飞书 user_id（企业级，跨该企业所有 app 一致）；考勤等按 user_id 推送的数据源用它反查 emp_id；应用无 user_id 权限时为空';
COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."id_source" IS '身份来源：own=本租户原生身份（可登录/考勤反查）；assoc=关联组织视角身份（仅供写多维表格人员字段，不参与登录/反查）';
COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."emp_id" IS '映射到的内部 EmpID；同一 emp_id 可有多条（跨飞书企业）';
COMMENT ON COLUMN plugin_linapro_employee_core_lark_identity."created_at" IS '创建时间';

CREATE INDEX IF NOT EXISTS idx_plugin_linapro_employee_core_lark_identity_emp
    ON plugin_linapro_employee_core_lark_identity ("emp_id");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_employee_core_lark_identity_user
    ON plugin_linapro_employee_core_lark_identity ("lark_app_id", "lark_user_id")
    WHERE "lark_user_id" <> '';