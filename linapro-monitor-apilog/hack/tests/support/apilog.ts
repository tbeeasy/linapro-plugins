import { createHmac } from 'node:crypto';
import { mkdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import {
  request,
  type APIRequestContext,
} from '@playwright/test';

import { config } from '@host-tests/fixtures/config';

// 与宿主 fixtures/plugin.ts 同口径：playwright 从 hack/tests 启动，仓库根在上两级。
// 不用 import.meta.url：插件目录没有 package.json，测试文件按 CJS 编译。
const repoRoot = path.resolve(process.cwd(), '../..');

/**
 * screenshotPath 返回截图落点：仓库根 temp/<YYYYMMDD>/<HHMMSS>-<name>.png（见 testing.md）。
 * 目录不存在时创建；时间取本地时间，与仓库既有截图同口径。
 */
export function screenshotPath(name: string): string {
  const now = new Date();
  const pad = (value: number) => String(value).padStart(2, '0');
  const day = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`;
  const time = `${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  const dir = path.resolve(repoRoot, 'temp', day);
  mkdirSync(dir, { recursive: true });
  return path.join(dir, `${time}-${name}.png`);
}

/** 插件的后台接口前缀：走宿主 /x/<plugin-id>/api/v1 代理。 */
export const apiLogPluginId = 'linapro-monitor-apilog';

export function apiLogAdminPath(pathName: string) {
  return `/x/${apiLogPluginId}/api/v1/${pathName.replace(/^\/+/, '')}`;
}

/**
 * readEmpJwtSecret 读取员工 JWT 签名密钥：优先环境变量，
 * 其次宿主开发配置（plugin.linapro-employee-core.emp_jwt_secret）。
 * 读不到时返回空串，调用方据此跳过员工端断言而不是伪造结果。
 */
export function readEmpJwtSecret(): string {
  const fromEnv = process.env.E2E_EMP_JWT_SECRET?.trim();
  if (fromEnv) return fromEnv;
  const candidates = [
    path.resolve(repoRoot, 'apps/lina-core/manifest/config/config.yaml'),
    path.resolve(repoRoot, 'apps/lina-core/manifest/config/config.online.yaml'),
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

/**
 * mintEmpToken 用与后端相同的算法（HS256、empId/tenantId 声明）签发一枚员工 JWT。
 *
 * 员工端接口依赖飞书登录换票，E2E 无法走通；直接签发真实令牌才能让 empcap.EmpAuth
 * 通过，从而验证「员工端请求会被本插件记录」这一核心行为。
 */
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

/** adminAccessToken 从已登录管理端的 storageState 中解析 accessToken。 */
export function adminAccessToken(storageStatePath: string): string {
  const raw = readFileSync(storageStatePath, 'utf8');
  const state = JSON.parse(raw) as {
    origins?: Array<{ localStorage?: Array<{ name: string; value: string }> }>;
  };
  const entries = (state.origins ?? []).flatMap(
    (origin) => origin.localStorage ?? [],
  );
  for (const entry of entries) {
    const token = extractJwt(entry.value);
    if (token) return token;
  }
  throw new Error(
    '管理端 storageState 中未找到 accessToken（请确认 globalSetup 已完成登录）',
  );
}

/** extractJwt 从一个 localStorage 值里找出 JWT：值可能是裸 token 或 JSON 包装。 */
function extractJwt(value: string): string {
  const pattern = /ey[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/;
  const direct = pattern.exec(value);
  if (direct) return direct[0];
  try {
    const parsed: unknown = JSON.parse(value);
    if (typeof parsed === 'string') return pattern.exec(parsed)?.[0] ?? '';
    return '';
  } catch {
    return '';
  }
}

/** newAdminApi 用已登录管理端的令牌建立 API 上下文。 */
export async function newAdminApi(
  storageStatePath: string,
): Promise<APIRequestContext> {
  return request.newContext({
    baseURL: config.backendBaseURL,
    extraHTTPHeaders: {
      Authorization: `Bearer ${adminAccessToken(storageStatePath)}`,
    },
  });
}

/** newEmpApi 建立带员工 JWT 的 API 上下文，用于直接调用员工端接口。 */
export async function newEmpApi(empToken: string): Promise<APIRequestContext> {
  return request.newContext({
    baseURL: config.backendBaseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${empToken}` },
  });
}

/**
 * resolveTestEmployee 解析 E2E 要用的员工身份。
 *
 * 员工端接口只认员工 JWT，而 JWT 里的 empId 必须是真实在职员工（EmpAuth 每请求
 * 重查员工状态）。因此由环境变量 E2E_APILOG_EMP_ID / E2E_APILOG_TENANT_ID 指定；
 * 未提供时返回 null，调用方跳过员工端断言而不是伪造结果。
 */
export function resolveTestEmployee(): { empId: number; tenantId: number } | null {
  const empId = Number.parseInt(process.env.E2E_APILOG_EMP_ID ?? '', 10);
  const tenantId = Number.parseInt(process.env.E2E_APILOG_TENANT_ID ?? '', 10);
  if (Number.isNaN(empId) || empId <= 0) return null;
  return { empId, tenantId: Number.isNaN(tenantId) ? 0 : tenantId };
}

/** 列表查询返回结构（只取出断言用到的字段）。 */
export interface ApiLogListResponse {
  items: Array<Record<string, unknown>>;
  total: number;
}

/**
 * queryApiLogs 用管理端令牌查询日志列表。
 *
 * 时间范围必填：表按 request_at 分区，后端会拒绝缺失范围的请求。
 */
export async function queryApiLogs(
  api: APIRequestContext,
  params: Record<string, string | number | undefined>,
): Promise<ApiLogListResponse> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      search.set(key, String(value));
    }
  }
  const response = await api.get(apiLogAdminPath('apilog/records'), {
    params: Object.fromEntries(search.entries()),
  });
  if (!response.ok()) {
    throw new Error(
      `查询接口请求日志失败: ${response.status()} ${await response.text()}`,
    );
  }
  const body = (await response.json()) as { data?: ApiLogListResponse };
  return body.data ?? { items: [], total: 0 };
}

/** 当前时间往前 windowMinutes 分钟到现在的查询窗口。 */
export function recentWindow(windowMinutes = 10): {
  beginTime: string;
  endTime: string;
} {
  const now = Date.now();
  return {
    beginTime: new Date(now - windowMinutes * 60 * 1000).toISOString(),
    endTime: new Date(now + 60 * 1000).toISOString(),
  };
}
