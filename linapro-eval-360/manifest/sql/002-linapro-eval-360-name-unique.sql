-- linapro-eval-360: 名称唯一约束（项目名全局唯一 / 问卷标题同项目内唯一）
--
-- 背景：同一系统内项目不允许重名；同一项目下问卷不允许重名。
-- 一律以「归一化后的名称」为比较口径——归一化 = 全角空格转半角 + 折叠内部连续空白 + 去首尾空白，
-- 大小写敏感（见 backend/internal/service/textutil/normalize.go）。
--
-- 注意：应用层只做预检（给出友好错误码），此处唯一索引才是并发一致性兜底。
-- 本文件用 IF NOT EXISTS，重复执行安全；但**不会**修改已存在表，也不会自动清理存量重名数据。

-- ============================================================================
-- 前置检查：存量重名数据会让建唯一索引直接失败。
-- 此处显式列出重复项并中止，交由人工决定保留哪一条（不静默删数据）。
-- ============================================================================
DO $$
DECLARE
    dup_project  TEXT;
    dup_title    TEXT;
BEGIN
    SELECT string_agg(format('id=%s name=%L', id, name), '; ' ORDER BY id)
      INTO dup_project
      FROM plugin_linapro_eval_360_project
     WHERE name IN (
               SELECT name
                 FROM plugin_linapro_eval_360_project
                GROUP BY name
               HAVING count(*) > 1
           );

    SELECT string_agg(format('project_id=%s title=%L', project_id, title), '; ' ORDER BY project_id, title)
      INTO dup_title
      FROM plugin_linapro_eval_360_questionnaire
     WHERE (project_id, title) IN (
               SELECT project_id, title
                 FROM plugin_linapro_eval_360_questionnaire
                GROUP BY project_id, title
               HAVING count(*) > 1
           );

    IF dup_project IS NOT NULL THEN
        RAISE EXCEPTION '存在重名项目，请先人工改名后再建唯一索引：%', dup_project;
    END IF;

    IF dup_title IS NOT NULL THEN
        RAISE EXCEPTION '存在同项目重名问卷，请先人工改名后再建唯一索引：%', dup_title;
    END IF;
END $$;

-- ============================================================================
-- 唯一索引：项目名全局唯一（插件为平台级，无 tenant_id，故不带租户前缀）
-- ============================================================================
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_project_name
    ON plugin_linapro_eval_360_project ("name");

-- ============================================================================
-- 唯一索引：问卷标题在同一项目内唯一
-- 红线：project_id 必须在键内——不同项目允许出现同名问卷。
-- ============================================================================
CREATE UNIQUE INDEX IF NOT EXISTS uq_eval_360_questionnaire_project_title
    ON plugin_linapro_eval_360_questionnaire ("project_id", "title");
