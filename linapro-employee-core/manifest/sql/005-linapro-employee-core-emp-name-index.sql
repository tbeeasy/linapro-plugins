-- ------------------------------------------------------------
-- 005 linapro-employee-core 迁移：employee 表增加姓名索引
-- 用途：支撑 empcap.MapByNamesAnyTenant 按姓名集合跨租户批量查询
-- （linapro-eval-360 评价关系按姓名录入、导入时一次 WhereIn 解析全部姓名并判定重名），
-- 避免该查询走全表扫描。普通索引（非唯一）：同名员工合法存在，重名由消费方判定。
-- 幂等：IF NOT EXISTS，可重复执行；卸载时随表一并删除，无需单独 DROP。
-- ------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_plugin_linapro_employee_core_emp_name
    ON plugin_linapro_employee_core_employee ("name");
