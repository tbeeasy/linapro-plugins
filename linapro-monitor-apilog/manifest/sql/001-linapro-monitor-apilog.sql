-- 001: linapro-monitor-apilog 数据结构
-- 001: linapro-monitor-apilog schema
--
-- 说明：
--   1. 本表按 request_at（请求开始时间）做月度范围分区，分区只用于查询剪枝，禁止 DROP / DETACH 分区（记录只增不删）。
--      created_at 是写库时间，由框架自动写入，不参与分区与筛选——异步写入的排队延迟不能改变时间筛选语义。
--   2. PostgreSQL 14 不支持分区表上的 identity 列，因此 id 由序列生成，主键为 (id, request_at)。
--   3. 卸载不删表；重装时 CREATE TABLE IF NOT EXISTS 会跳过建表，
--      新增列只能靠下面每一列对应的 ADD COLUMN IF NOT EXISTS 收敛。
--      改列 / 改索引 / 改约束必须显式写 ALTER，否则旧表会停在上一个版本的结构。

-- 记录主键序列。id 全局唯一，只是不能单独做分区表的主键。
CREATE SEQUENCE IF NOT EXISTS plugin_linapro_monitor_apilog_id_seq;

CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog (
    "id"            BIGINT      NOT NULL DEFAULT nextval('plugin_linapro_monitor_apilog_id_seq'),
    "request_at"    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "request_id"    VARCHAR(64)  NOT NULL DEFAULT '',
    "route_owner"   VARCHAR(100) NOT NULL DEFAULT '',
    "method"        VARCHAR(10)  NOT NULL DEFAULT '',
    "path"          VARCHAR(500) NOT NULL DEFAULT '',
    "route_path"    VARCHAR(255) NOT NULL DEFAULT '',
    "query"         TEXT         NOT NULL DEFAULT '',
    "client_ip"     VARCHAR(64)  NOT NULL DEFAULT '',
    "user_agent"    VARCHAR(500) NOT NULL DEFAULT '',
    "caller_kind"   VARCHAR(32)  NOT NULL DEFAULT '',
    "caller_id"     VARCHAR(128) NOT NULL DEFAULT '',
    "caller_tenant" INT          NOT NULL DEFAULT 0,
    "caller_name"   VARCHAR(64)  NOT NULL DEFAULT '',
    "caller_no"     VARCHAR(64)  NOT NULL DEFAULT '',
    "status_code"   INT          NOT NULL DEFAULT 0,
    "cost_ms"       INT          NOT NULL DEFAULT 0,
    "req_headers"   TEXT         NOT NULL DEFAULT '',
    "req_body"      TEXT         NOT NULL DEFAULT '',
    "resp_body"     TEXT         NOT NULL DEFAULT '',
    "error_msg"     TEXT         NOT NULL DEFAULT '',
    "created_at"    TIMESTAMPTZ,
    CONSTRAINT plugin_linapro_monitor_apilog_pkey PRIMARY KEY ("id", "request_at")
) PARTITION BY RANGE ("request_at");

-- 每一列都写一条 ADD COLUMN IF NOT EXISTS：卸载不删表，重装时 CREATE TABLE IF NOT EXISTS
-- 会跳过建表，新增列只能靠这些 ALTER 收敛。加列这件事由它们完全覆盖。
--
-- 顺序要求：这些 ALTER 必须排在 COMMENT ON COLUMN 之前。
-- 旧表若缺某一列，COMMENT ON 会直接报「column does not exist」并中断整个安装，
-- 补列的语句就永远轮不到执行——这正是「卸载后重装」时唯一会出问题的场景。
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "id"            BIGINT      NOT NULL DEFAULT nextval('plugin_linapro_monitor_apilog_id_seq');
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "request_at"    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "request_id"    VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "route_owner"   VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "method"        VARCHAR(10)  NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "path"          VARCHAR(500) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "route_path"    VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "query"         TEXT         NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "client_ip"     VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "user_agent"    VARCHAR(500) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "caller_kind"   VARCHAR(32)  NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "caller_id"     VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "caller_tenant" INT          NOT NULL DEFAULT 0;
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "caller_name"   VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "caller_no"     VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "status_code"   INT          NOT NULL DEFAULT 0;
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "cost_ms"       INT          NOT NULL DEFAULT 0;
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "req_headers"   TEXT         NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "req_body"      TEXT         NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "resp_body"     TEXT         NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "error_msg"     TEXT         NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_monitor_apilog ADD COLUMN IF NOT EXISTS "created_at"    TIMESTAMPTZ;

COMMENT ON TABLE plugin_linapro_monitor_apilog IS '接口请求日志：非管理后台接口的请求与响应，只增不删';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."id" IS '记录 ID，由序列生成（分区表不支持 identity 列）';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."request_at" IS '分区键：请求开始时间，与 cost_ms 同一基准；列表筛选、排序与详情定位都用它';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."request_id" IS '请求 ID，与业务日志互查';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."route_owner" IS '路由归属插件 ID，用于按插件筛选与排除名单匹配';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."method" IS 'HTTP 方法';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."path" IS '实际请求路径';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."route_path" IS '路由模式，例如 /x/.../task/{id}；动态插件取真实路径';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."query" IS '原始查询串，敏感参数已脱敏';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."client_ip" IS '客户端 IP';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."user_agent" IS 'User-Agent';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."caller_kind" IS '调用方类型：emp=员工 app_key=外部系统';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."caller_id" IS '调用方标识：emp 时为 EmpID，app_key 时为 app_key；唯一的机器关联键';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."caller_tenant" IS '调用方租户 ID，无租户语义时为 0';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."caller_name" IS '调用方姓名快照，只用于显示与按人搜索';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."caller_no" IS '调用方工号快照，只用于显示与按人搜索';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."status_code" IS 'HTTP 状态码';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."cost_ms" IS '耗时，单位毫秒';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."req_headers" IS '请求头 JSON，已脱敏';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."req_body" IS '完整请求体，已脱敏';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."resp_body" IS '完整响应体，已脱敏';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."error_msg" IS '错误信息';
COMMENT ON COLUMN plugin_linapro_monitor_apilog."created_at" IS '写库时间，由框架自动写入';

-- 索引建在父表上，PG 会自动为已存在和将来新建的每个分区生成同名索引（PG 11 起）。
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_monitor_apilog_request_at ON plugin_linapro_monitor_apilog ("request_at");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_monitor_apilog_owner_request_at ON plugin_linapro_monitor_apilog ("route_owner", "request_at");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_monitor_apilog_caller_id_request_at ON plugin_linapro_monitor_apilog ("caller_id", "request_at");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_monitor_apilog_caller_no_request_at ON plugin_linapro_monitor_apilog ("caller_no", "request_at");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_monitor_apilog_caller_name_request_at ON plugin_linapro_monitor_apilog ("caller_name", "request_at");
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_monitor_apilog_request_id ON plugin_linapro_monitor_apilog ("request_id");

-- 初始分区：当前月 + 未来 6 个月，共 7 个。
-- 与插件每日任务共用同一套「左闭右开、按月」的边界算法（见 backend/internal/service/partition）。
-- 刻意不建 DEFAULT 分区：一旦有行落进 DEFAULT，之后再为同一范围建正式分区会失败，
-- 修复只能把行移出去（= DELETE + INSERT），直接违反「只增不删」。
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202611 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202612 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202701 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2027-01-01 00:00:00+00') TO ('2027-02-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202702 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2027-02-01 00:00:00+00') TO ('2027-03-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202703 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2027-03-01 00:00:00+00') TO ('2027-04-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202704 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2027-04-01 00:00:00+00') TO ('2027-05-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS plugin_linapro_monitor_apilog_p202705 PARTITION OF plugin_linapro_monitor_apilog FOR VALUES FROM ('2027-05-01 00:00:00+00') TO ('2027-06-01 00:00:00+00');
