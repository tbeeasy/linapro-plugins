-- ------------------------------------------------------------
-- 001 linapro-lark-push api key SQL file
-- 001 linapro-lark-push 签名凭据 SQL 文件
-- Purpose: Stores HMAC signature credentials for the external push API, managed from the admin page.
-- 用途：存储对外推送接口的 HMAC 签名凭据，由后台管理页维护；签名中间件每次请求按 app_key 查询。
-- ------------------------------------------------------------

CREATE TABLE IF NOT EXISTS plugin_linapro_lark_push_api_key (
    "id"         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "name"       VARCHAR(64)  NOT NULL DEFAULT '',
    "app_key"    VARCHAR(64)  NOT NULL,
    "secret"     VARCHAR(128) NOT NULL,
    "push_group" VARCHAR(64)  NOT NULL,
    "chat_id"    VARCHAR(128) NOT NULL DEFAULT '',
    "chat_push_app_id" VARCHAR(64) NOT NULL DEFAULT '',
    "status"     SMALLINT     NOT NULL DEFAULT 1,
    "remark"     VARCHAR(500) NOT NULL DEFAULT '',
    "created_at" TIMESTAMPTZ,
    "updated_at" TIMESTAMPTZ,
    "deleted_at" TIMESTAMPTZ
);

-- 群聊目标由后台配置在凭据上（FB-10），调用方不能自行传 chat_id；v0.1.1 已建表的环境经升级补列。
ALTER TABLE plugin_linapro_lark_push_api_key ADD COLUMN IF NOT EXISTS "chat_id" VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_lark_push_api_key ADD COLUMN IF NOT EXISTS "chat_push_app_id" VARCHAR(64) NOT NULL DEFAULT '';

COMMENT ON TABLE plugin_linapro_lark_push_api_key IS 'Lark push external API signature credential table';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."id" IS 'Credential ID';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."name" IS 'Caller display name';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."app_key" IS 'Public app key carried in the X-App-Key header';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."secret" IS 'HMAC-SHA256 signing secret';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."push_group" IS 'Bound push group name in push_groups config';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."chat_id" IS 'Feishu group chat_id pushed with every request of this credential; empty means no group push';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."chat_push_app_id" IS 'Push app ID in the bound group used for the chat; empty means the group default (single push app)';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."status" IS 'Status from dict sys_normal_disable: 1=normal 0=disabled';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."remark" IS 'Remark';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."created_at" IS 'Creation time';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."updated_at" IS 'Update time';
COMMENT ON COLUMN plugin_linapro_lark_push_api_key."deleted_at" IS 'Deletion time';

-- 签名中间件按 app_key 查询存活凭据；删除后同名 app_key 可重建，故唯一约束只覆盖未删除行。
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_linapro_lark_push_api_key_app_key_alive
    ON plugin_linapro_lark_push_api_key ("app_key")
    WHERE "deleted_at" IS NULL;
-- 管理页列表按分组、状态过滤并按更新时间倒序。
CREATE INDEX IF NOT EXISTS idx_plugin_linapro_lark_push_api_key_group_status_alive
    ON plugin_linapro_lark_push_api_key ("push_group", "status")
    WHERE "deleted_at" IS NULL;
