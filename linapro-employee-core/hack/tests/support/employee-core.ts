// employee-core E2E 辅助：后台通讯录管理页的接口路径、种子员工写入与清理。
//
// 员工行由 Moka 同步写入、后台没有新增接口（见 design D25），因此 E2E 无法经 UI
// 造数；这里直接向插件表插入带唯一工号/姓名的种子行，用例结束后按租户删除。
// 只写 employee 与 lark_identity 两张表，不触碰同步任务的其他状态。
import { mkdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { request, type APIRequestContext, type Page } from '@playwright/test';

import { config, pluginApiPath } from '../../../../../../hack/tests/fixtures/config';
import {
  execPgSQL,
  pgEscapeLiteral,
  queryPgRows,
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
  /** 三个任职日期（YYYY-MM-DD），未传时为空（库中 NULL）。 */
  onBoardingDate: string;
  endProbationDate: string;
  leaveDate: string;
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
  onBoardingDate?: string;
  endProbationDate?: string;
  leaveDate?: string;
}): SeedEmployee {
  const employeeNo = pgEscapeLiteral(options.employeeNo);
  const name = pgEscapeLiteral(options.name);
  const status = options.status ?? 1;
  // 任职日期为可空 DATE：未传时写 NULL，传值时按 SQL 日期字面量写入。
  const dateSql = (value?: string) =>
    value ? `'${pgEscapeLiteral(value)}'` : 'NULL';
  const empId = Number.parseInt(
    queryPgScalar(
      `INSERT INTO ${employeeTable} ("tenant_id","name","moka_employee_no","status","sys_user_id","on_boarding_date","end_probation_date","leave_date","created_at","updated_at")
       VALUES (${options.tenantId},'${name}','${employeeNo}',${status},0,${dateSql(options.onBoardingDate)},${dateSql(options.endProbationDate)},${dateSql(options.leaveDate)},now(),now())
       ON CONFLICT ("tenant_id","moka_employee_no") WHERE "deleted_at" IS NULL AND "moka_employee_no" <> ''
       DO UPDATE SET "name" = EXCLUDED."name", "status" = EXCLUDED."status",
                     "on_boarding_date" = EXCLUDED."on_boarding_date",
                     "end_probation_date" = EXCLUDED."end_probation_date",
                     "leave_date" = EXCLUDED."leave_date", "updated_at" = now()
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
    onBoardingDate: options.onBoardingDate ?? '',
    endProbationDate: options.endProbationDate ?? '',
    leaveDate: options.leaveDate ?? '',
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

/** 员工列表菜单的权限点键名。 */
export const employeeMenuPerms = {
  edit: 'linapro-employee-core:employee:edit',
  list: 'linapro-employee-core:employee:list',
  query: 'linapro-employee-core:employee:query',
} as const;

/** 通讯录管理菜单树在 sys_menu 中的键名。 */
export const employeeMenuKeys = {
  contacts: 'plugin:linapro-employee-core:contacts',
  edit: 'plugin:linapro-employee-core:contacts:employees:edit',
  employees: 'plugin:linapro-employee-core:contacts:employees',
  query: 'plugin:linapro-employee-core:contacts:employees:detail',
} as const;

/** menuIDsByKey 把菜单键名解析成 sys_menu 主键，缺失即报错（防止静默跳过）。 */
export function menuIDsByKey(keys: readonly string[]): number[] {
  return keys.map((key) => {
    const id = queryPgScalar(
      `SELECT id FROM sys_menu WHERE menu_key = '${pgEscapeLiteral(key)}' AND deleted_at IS NULL LIMIT 1;`,
    );
    if (!id) {
      throw new Error(`未找到菜单: ${key}（请确认插件已安装并同步菜单）`);
    }
    return Number.parseInt(id, 10);
  });
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

/** createLimitedRole 建一个只授予指定菜单的测试角色，返回角色 ID。 */
export async function createLimitedRole(
  adminApi: APIRequestContext,
  options: { menuIds: number[]; name: string; key: string },
): Promise<number> {
  // newAdminApi 的 baseURL 是后端根地址（插件路径 /x/<id>/api/v1/... 需要它），
  // 宿主接口必须显式带上 /api/v1 前缀。
  const response = await adminApi.post('/api/v1/role', {
    data: {
      dataScope: 1,
      key: options.key,
      menuIds: options.menuIds,
      name: options.name,
      remark: 'employee-core E2E 权限反向用例专用角色',
      sort: 99,
      status: 1,
    },
  });
  if (!response.ok()) {
    throw new Error(`创建角色失败: ${options.key}, status=${response.status()}`);
  }
  const payload = (await response.json()) as { data?: { id?: number } };
  const id = payload?.data?.id;
  if (!id) {
    throw new Error(`创建角色未返回 ID: ${options.key}`);
  }
  return id;
}

/** createLimitedUser 建一个只挂指定角色的测试用户，返回用户 ID。 */
export async function createLimitedUser(
  adminApi: APIRequestContext,
  options: { nickname: string; password: string; roleIds: number[]; username: string },
): Promise<number> {
  const response = await adminApi.post('/api/v1/user', {
    data: {
      nickname: options.nickname,
      password: options.password,
      roleIds: options.roleIds,
      status: 1,
      username: options.username,
    },
  });
  if (!response.ok()) {
    throw new Error(`创建用户失败: ${options.username}, status=${response.status()}`);
  }
  const payload = (await response.json()) as { data?: { id?: number } };
  const id = payload?.data?.id;
  if (!id) {
    throw new Error(`创建用户未返回 ID: ${options.username}`);
  }
  return id;
}

/** cleanupRoleAndUser 按用户名/角色键删除测试用户与角色，含授权关系。 */
export function cleanupRoleAndUser(username: string, roleKey: string) {
  const escapedUser = pgEscapeLiteral(username);
  const escapedRole = pgEscapeLiteral(roleKey);
  execPgSQL(
    `DELETE FROM sys_user_role WHERE user_id IN (SELECT id FROM sys_user WHERE username = '${escapedUser}');
     DELETE FROM sys_user WHERE username = '${escapedUser}';
     DELETE FROM sys_role_menu WHERE role_id IN (SELECT id FROM sys_role WHERE "key" = '${escapedRole}');
     DELETE FROM sys_role WHERE "key" = '${escapedRole}';`,
  );
}

/**
 * captureScreenshot 按 .agents/rules/testing.md 存截图：
 * 项目根 temp/<日期>/<时:分:秒>-<描述>.png。
 */
export async function captureScreenshot(page: Page, description: string) {
  const stamp = new Date();
  const datePart = [
    stamp.getFullYear(),
    String(stamp.getMonth() + 1).padStart(2, '0'),
    String(stamp.getDate()).padStart(2, '0'),
  ].join('');
  const timePart = [
    String(stamp.getHours()).padStart(2, '0'),
    String(stamp.getMinutes()).padStart(2, '0'),
    String(stamp.getSeconds()).padStart(2, '0'),
  ].join('');
  const dir = path.resolve(process.cwd(), '../..', 'temp', datePart);
  mkdirSync(dir, { recursive: true });
  await page.screenshot({ path: path.join(dir, `${timePart}-${description}.png`) });
}

/** tableRows 读取某张表在给定条件下的行数（用于断言清理是否干净）。 */
export function countRows(sql: string): number {
  const rows = queryPgRows(sql);
  return rows.length === 0 ? 0 : Number.parseInt(rows[0]!, 10);
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
