// employee-core E2E 辅助：后台通讯录管理页的接口路径、种子员工写入与清理。
//
// 员工行由 Moka 同步写入、后台没有新增接口（见 design D25），因此 E2E 无法经 UI
// 造数；这里直接向插件表插入带唯一工号/姓名的种子行，用例结束后按租户删除。
// 只写 employee 与 lark_identity 两张表，不触碰同步任务的其他状态。
import { readFileSync } from 'node:fs';

import { request, type APIRequestContext } from '@playwright/test';

import { config, pluginApiPath } from '../../../../../../hack/tests/fixtures/config';
import {
  execPgSQL,
  pgEscapeLiteral,
  queryPgScalar,
} from '../../../../../../hack/tests/support/postgres';

export const employeeCorePluginId = 'linapro-employee-core';

/** 后台员工接口路径（走宿主 /x/<plugin-id>/api/v1 代理）。 */
export function employeeApiPath(pathName = '') {
  return pluginApiPath(employeeCorePluginId, `contacts/employees${pathName}`);
}

export const employeeTable = 'plugin_linapro_employee_core_employee';
export const larkIdentityTable = 'plugin_linapro_employee_core_lark_identity';

/** 一条种子员工。 */
export interface SeedEmployee {
  empId: number;
  name: string;
  employeeNo: string;
  tenantId: number;
  status: number;
  appId: string;
  openId: string;
}

/**
 * seedEmployee 插入一名员工与一条 own 飞书身份，返回其 EmpID。
 * 工号与 open_id 都带时间戳，避免与历史种子撞唯一键。
 */
export function seedEmployee(options: {
  tenantId: number;
  name: string;
  employeeNo: string;
  status?: number;
  withOwnIdentity?: boolean;
}): SeedEmployee {
  const employeeNo = pgEscapeLiteral(options.employeeNo);
  const name = pgEscapeLiteral(options.name);
  const status = options.status ?? 1;
  const empId = Number.parseInt(
    queryPgScalar(
      `INSERT INTO ${employeeTable} ("tenant_id","name","moka_employee_no","status","sys_user_id","created_at","updated_at")
       VALUES (${options.tenantId},'${name}','${employeeNo}',${status},0,now(),now())
       ON CONFLICT ("tenant_id","moka_employee_no") WHERE "deleted_at" IS NULL AND "moka_employee_no" <> ''
       DO UPDATE SET "name" = EXCLUDED."name", "status" = EXCLUDED."status", "updated_at" = now()
       RETURNING "id";`,
    ),
    10,
  );

  const appId = `cli_e2e_${options.tenantId}`;
  const openId = `ou_e2e_${options.employeeNo}`;
  if (options.withOwnIdentity !== false) {
    execPgSQL(
      `INSERT INTO ${larkIdentityTable} ("lark_app_id","lark_open_id","lark_user_id","id_source","emp_id","created_at")
       VALUES ('${pgEscapeLiteral(appId)}','${pgEscapeLiteral(openId)}','uid_${pgEscapeLiteral(options.employeeNo)}','own',${empId},now())
       ON CONFLICT ("lark_app_id","lark_open_id") DO UPDATE SET "emp_id" = EXCLUDED."emp_id", "id_source" = 'own';`,
    );
  }

  return {
    empId,
    name: options.name,
    employeeNo: options.employeeNo,
    tenantId: options.tenantId,
    status,
    appId,
    openId,
  };
}

/** cleanupEmployees 删除某租户的种子数据：先身份行（无 tenant_id），再员工行。 */
export function cleanupEmployees(tenantId: number) {
  execPgSQL(
    `DELETE FROM ${larkIdentityTable}
      WHERE "emp_id" IN (SELECT "id" FROM ${employeeTable} WHERE "tenant_id" = ${tenantId});
     DELETE FROM ${employeeTable} WHERE "tenant_id" = ${tenantId};`,
  );
}

/** newAdminApi 用管理端会话令牌建立 API 上下文（与 UI 同一身份与租户）。 */
export async function newAdminApi(storageStatePath: string): Promise<APIRequestContext> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${adminAccessToken(storageStatePath)}`,
  };
  const tenant = activeTenant(storageStatePath);
  if (tenant) {
    headers['X-Tenant-Code'] = tenant.code;
  }
  return request.newContext({
    baseURL: config.backendBaseURL,
    extraHTTPHeaders: headers,
  });
}

/** adminAccessToken 从管理端 storageState 里按值形状找出 JWT，不猜键名。 */
export function adminAccessToken(storageStatePath: string): string {
  for (const entry of storageEntries(storageStatePath)) {
    const direct = /^[\w-]+\.[\w-]+\.[\w-]+$/u.test(entry.value) ? entry.value : '';
    if (direct) {
      return direct;
    }
    const match = /"(eyJ[\w-]+\.[\w-]+\.[\w-]+)"/u.exec(entry.value);
    if (match?.[1]) {
      return match[1];
    }
  }
  throw new Error('管理端 storageState 中未找到 accessToken（请确认 globalSetup 已完成登录）');
}

/** activeTenant 读取会话中当前选中的租户；未选择时返回 null。 */
function activeTenant(storageStatePath: string): null | { code: string; id: number } {
  for (const entry of storageEntries(storageStatePath)) {
    if (!entry.name.includes('tenant-state')) {
      continue;
    }
    try {
      const parsed = JSON.parse(entry.value) as {
        currentTenant?: { code?: string; id?: number };
      };
      if (parsed.currentTenant?.code) {
        return { code: parsed.currentTenant.code, id: parsed.currentTenant.id ?? 0 };
      }
    } catch {
      // 值不是 JSON 时忽略该条目。
    }
  }
  return null;
}

/** storageEntries 列出 storageState 里全部 localStorage 条目。 */
function storageEntries(storageStatePath: string): Array<{ name: string; value: string }> {
  const raw = readFileSync(storageStatePath, 'utf8');
  const state = JSON.parse(raw) as {
    origins?: Array<{ localStorage?: Array<{ name: string; value: string }> }>;
  };
  return (state.origins ?? []).flatMap((origin) => origin.localStorage ?? []);
}
