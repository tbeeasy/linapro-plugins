import { createHmac } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { request, type APIRequestContext, type Page } from '@playwright/test';
import * as XLSX from 'xlsx';

import { config, pluginApiPath } from '../../../../../../hack/tests/fixtures/config';

const supportDir = path.dirname(fileURLToPath(import.meta.url));
const pluginRoot = path.resolve(supportDir, '../../..');
const tempDir = path.resolve(pluginRoot, 'temp');

// 插件的 API 前缀：走宿主 /x/<plugin-id>/api/v1 代理。
export const evalPluginId = 'linapro-eval-360';

export function evalApiPath(pathName: string) {
  return pluginApiPath(evalPluginId, `eval/admin/${pathName}`);
}

/** 解析后的 xlsx 工作表：表头 + 数据行（按单元格引用补空列后的矩阵）。 */
export interface ParsedSheet {
  header: string[];
  rows: string[][];
  /** 与 rows 同形的单元格类型（SheetJS 的 `t`：`n` 数字 / `s` 文本 / 空串表示空单元格）。 */
  types: string[][];
}

// readXlsxSheet 用同一个解析器读取导出产物：按 A1 引用定位列号补空列，
// 保证断言按表头名取值而不是硬编码列序号。
export function readXlsxSheet(data: Buffer): ParsedSheet {
  const workbook = XLSX.read(data, { type: 'buffer' });
  const sheetName = workbook.SheetNames[0];
  if (!sheetName) {
    throw new Error('导出的 xlsx 没有任何工作表');
  }
  const sheet = workbook.Sheets[sheetName]!;
  const range = XLSX.utils.decode_range(sheet['!ref'] ?? 'A1:A1');
  const rows: string[][] = [];
  const types: string[][] = [];
  for (let r = range.s.r; r <= range.e.r; r += 1) {
    const row: string[] = [];
    const rowTypes: string[] = [];
    for (let c = range.s.c; c <= range.e.c; c += 1) {
      const cell = sheet[XLSX.utils.encode_cell({ c, r })];
      const empty = cell?.v === undefined || cell?.v === null;
      row.push(empty ? '' : String(cell.v));
      rowTypes.push(empty ? '' : String(cell.t));
    }
    rows.push(row);
    types.push(rowTypes);
  }
  const [header = [], ...dataRows] = rows;
  return { header, rows: dataRows, types: types.slice(1) };
}

/** 取数据行中某列的单元格类型（`n` 数字 / `s` 文本 / 空串表示空单元格）。 */
export function cellTypeByHeader(sheet: ParsedSheet, rowIndex: number, name: string): string {
  const col = sheet.header.indexOf(name);
  return col < 0 ? '' : (sheet.types[rowIndex]?.[col] ?? '');
}

/** 把一行按表头投影为「列名 → 值」。 */
export function rowByHeader(sheet: ParsedSheet, row: string[]): Record<string, string> {
  const out: Record<string, string> = {};
  sheet.header.forEach((name, index) => {
    if (!name) return;
    out[name] = row[index] ?? '';
  });
  return out;
}

/** 评价关系导入行：被评估人 / 评价者取自在职员工（导入只写姓名，工号仅用于断言），职务/部门可选。 */
export interface RelationImportRow {
  evaluatee: { employeeNo: string; name: string };
  evaluator: { employeeNo: string; name: string };
  role: string;
  jobTitle?: string;
  department?: string;
}

// buildRelationWorkbook 生成与生产模板同结构的评价关系导入 xlsx
//（列：被评估人姓名 / 被评估人职务 / 被评估人部门 / 评价者姓名 / 角色；不含工号，后端按姓名匹配），
// 返回落盘路径。
export function buildRelationWorkbook(rows: RelationImportRow[]): string {
  const sheet = XLSX.utils.aoa_to_sheet([
    ['被评估人姓名', '被评估人职务', '被评估人部门', '评价者姓名', '角色'],
    ...rows.map((r) => [
      r.evaluatee.name,
      r.jobTitle ?? '',
      r.department ?? '',
      r.evaluator.name,
      r.role,
    ]),
  ]);
  const workbook = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(workbook, sheet, 'Sheet1');
  mkdirSync(tempDir, { recursive: true });
  const filePath = path.join(tempDir, `${Date.now()}-eval360-relations.xlsx`);
  writeFileSync(filePath, XLSX.write(workbook, { type: 'buffer', bookType: 'xlsx' }));
  return filePath;
}

// ---- 员工端会话 ----

// readEmpJwtSecret 读取员工 JWT 签名密钥：优先环境变量，
// 其次宿主开发配置（plugin.linapro-employee-core.emp_jwt_secret）。
// 读不到时返回空串，调用方据此跳过员工端断言而不是伪造结果。
export function readEmpJwtSecret(): string {
  const fromEnv = process.env.E2E_EMP_JWT_SECRET?.trim();
  if (fromEnv) return fromEnv;
  const candidates = [
    path.resolve(pluginRoot, '../../lina-core/manifest/config/config.yaml'),
    path.resolve(pluginRoot, '../../lina-core/manifest/config/config.online.yaml'),
  ];
  for (const candidate of candidates) {
    try {
      const text = readFileSync(candidate, 'utf8');
      const match = /^\s*emp_jwt_secret:\s*"?([^"\s]+)"?\s*$/mu.exec(text);
      if (match?.[1]) return match[1];
    } catch {
      // 文件不存在则尝试下一个来源。
    }
  }
  return '';
}

// mintEmpToken 用与后端相同的算法（HS256、2 小时 TTL、empId/tenantId 声明）
// 签发一枚员工 JWT。员工端登录依赖飞书 OAuth，E2E 无法走通；
// 网页端本身即为「贴 token 到 localStorage」而写（见 answer.vue 顶部注释）。
export function mintEmpToken(secret: string, empId: number, tenantId: number): string {
  const base64url = (input: Buffer | string) =>
    Buffer.from(input).toString('base64url');
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const payload = base64url(
    JSON.stringify({ empId, tenantId, exp: now + 2 * 60 * 60, iat: now }),
  );
  const signature = createHmac('sha256', secret)
    .update(`${header}.${payload}`)
    .digest('base64url');
  return `${header}.${payload}.${signature}`;
}

/** 在浏览器上下文注入员工会话（员工端 store 直接读这两个 localStorage 键）。 */
export async function seedEmployeeSession(
  page: Page,
  token: string,
  larkAppId = 'e2e-app',
) {
  await page.addInitScript(
    (seed: unknown) => {
      const { token: tk, larkAppId: app } = seed as { larkAppId: string; token: string };
      localStorage.setItem('linapro:emp-auth-token', tk);
      localStorage.setItem('linapro:emp-lark-app-id', app);
    },
    { token, larkAppId },
  );
}

// ---- 管理端 API 直连（供测试准备与断言用） ----

// adminAccessToken 从已登录管理端的 storageState 中解析 accessToken。
// 令牌由管理端自己写入 localStorage，E2E 不重复实现其键名规则：按值形状找到 JWT 即可。
export function adminAccessToken(storageStatePath: string): string {
  const raw = readFileSync(storageStatePath, 'utf8');
  const state = JSON.parse(raw) as {
    origins?: Array<{ localStorage?: Array<{ name: string; value: string }> }>;
  };
  const entries = (state.origins ?? []).flatMap((origin) => origin.localStorage ?? []);
  for (const entry of entries) {
    const token = extractJwt(entry.value);
    if (token) return token;
  }
  throw new Error('管理端 storageState 中未找到 accessToken（请确认 globalSetup 已完成登录）');
}

// extractJwt 从一个 localStorage 值里找出 JWT：值可能是裸 token 或 JSON 包装。
function extractJwt(value: string): string {
  const direct = /^[\w-]+\.[\w-]+\.[\w-]+$/u.test(value) ? value : '';
  if (direct) return direct;
  const match = /"(eyJ[\w-]+\.[\w-]+\.[\w-]+)"/u.exec(value);
  return match?.[1] ?? '';
}

// newAdminApi 用已登录管理端的令牌建立 API 上下文，复用同一会话与权限，
// 不额外维护凭据、也不伪造身份。
// 多租户部署下管理端请求还会带 X-Tenant-Code，这里按会话里实际选中的租户补齐，
// 否则插件的租户过滤会解析到平台租户而查不到刚创建的项目。
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

// activeTenant 读取会话中当前选中的租户（未选择租户时返回 null）。
function activeTenant(storageStatePath: string): { code: string; id: number } | null {
  const raw = readFileSync(storageStatePath, 'utf8');
  const state = JSON.parse(raw) as {
    origins?: Array<{ localStorage?: Array<{ name: string; value: string }> }>;
  };
  const entries = (state.origins ?? []).flatMap((origin) => origin.localStorage ?? []);
  for (const entry of entries) {
    if (!entry.name.includes('tenant-state')) continue;
    try {
      const parsed = JSON.parse(entry.value) as {
        currentTenant?: { code?: string; id?: number };
      };
      const current = parsed.currentTenant;
      if (current?.code) {
        return { code: current.code, id: current.id ?? 0 };
      }
    } catch {
      // 值不是 JSON 时忽略该条目。
    }
  }
  return null;
}

// resolveTenantId 解析管理端会话所属租户 id：优先取会话里选中的租户，
// 其次环境变量覆盖，最后回退平台默认租户 0（单租户部署即平台租户）。
export function resolveTenantId(storageStatePath: string): number {
  const tenant = activeTenant(storageStatePath);
  if (tenant) return tenant.id;
  const fromEnv = Number.parseInt(process.env.E2E_TENANT_ID ?? '', 10);
  return Number.isNaN(fromEnv) ? 0 : fromEnv;
}

/**
 * 取 count 名在职员工的工号与姓名（评价关系导入/员工端断言的真实人员）。
 *
 * 员工搜索接口只按工号精确匹配（工号全公司唯一，见 relation.SearchEmployees），
 * 无法按模糊关键字枚举员工；因此由环境变量 E2E_EVAL360_EMPLOYEE_NOS 提供
 * 逗号分隔的在职员工工号（如 GZ000071,CJ000003,GZ000072），逐个经接口解析为真实员工。
 * 发布要求全部评价者都能收到飞书卡片（FB-30），故这些工号须在 linapro-eval-360 推送分组的
 * 任一企业有飞书身份，否则发布会被拦截。
 */
export async function pickActiveEmployees(
  api: APIRequestContext,
  count: number,
): Promise<Array<{ empId: number; employeeNo: string; name: string }>> {
  const employeeNos = (process.env.E2E_EVAL360_EMPLOYEE_NOS ?? '')
    .split(',')
    .map((no) => no.trim())
    .filter(Boolean);
  const list: Array<{ empId: number; employeeNo: string; name: string }> = [];
  for (const employeeNo of employeeNos) {
    if (list.length >= count) {
      break;
    }
    const response = await api.get(evalApiPath('relation/search-employees'), {
      params: { keyword: employeeNo },
    });
    if (!response.ok()) {
      throw new Error(`搜索员工失败：HTTP ${response.status()}`);
    }
    const body = await response.json();
    const hit = (body?.data?.list ?? [])[0];
    if (hit) {
      list.push(hit);
    }
  }
  if (list.length < count) {
    throw new Error(
      `在职员工不足 ${count} 名（实际 ${list.length}）：请通过 E2E_EVAL360_EMPLOYEE_NOS 提供逗号分隔的在职员工工号`,
    );
  }
  return list;
}

/**
 * 取一个「有两名及以上在职员工同名」的姓名（FB-32 重名拒绝路径用）：由环境变量
 * E2E_EVAL360_DUPLICATE_NAME 提供（员工数据来自 Moka 同步，测试无法自行造同名员工）。
 * 未配置时返回 null，调用方据此跳过该子断言。
 */
export function pickDuplicateName(): null | string {
  const name = (process.env.E2E_EVAL360_DUPLICATE_NAME ?? '').trim();
  return name || null;
}

/**
 * 取一名「在职但收不到飞书卡片」的员工（FB-30 发布失败路径用）：工号由环境变量
 * E2E_EVAL360_UNREACHABLE_EMPLOYEE_NO 提供，须为在职员工且在 linapro-eval-360 推送分组的
 * 所有企业都没有飞书身份。未配置时返回 null，调用方据此跳过该子断言。
 */
export async function pickUnreachableEmployee(
  api: APIRequestContext,
): Promise<null | { empId: number; employeeNo: string; name: string }> {
  const employeeNo = (process.env.E2E_EVAL360_UNREACHABLE_EMPLOYEE_NO ?? '').trim();
  if (!employeeNo) {
    return null;
  }
  const response = await api.get(evalApiPath('relation/search-employees'), {
    params: { keyword: employeeNo },
  });
  if (!response.ok()) {
    throw new Error(`搜索员工失败：HTTP ${response.status()}`);
  }
  const hit = ((await response.json())?.data?.list ?? [])[0];
  if (!hit) {
    throw new Error(`E2E_EVAL360_UNREACHABLE_EMPLOYEE_NO=${employeeNo} 不是在职员工`);
  }
  return hit;
}

// ---- 管理端 API 直连（供测试准备与断言用） ----

/** 取某项目下第一份问卷的 id。 */
export async function findQuestionnaireId(
  api: APIRequestContext,
  projectId: number,
): Promise<number> {
  const response = await api.get(evalApiPath('questionnaire'), {
    params: { projectId },
  });
  const body = await response.json();
  const list: Array<{ id: number }> = body?.data?.list ?? [];
  if (list.length === 0) {
    throw new Error(`项目 ${projectId} 下没有问卷`);
  }
  return list[0]!.id;
}
