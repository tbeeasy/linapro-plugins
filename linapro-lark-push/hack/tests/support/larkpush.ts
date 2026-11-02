import { createHmac, randomBytes } from 'node:crypto';
import { readFileSync } from 'node:fs';

import { request, type APIRequestContext } from '@playwright/test';

import { config, pluginApiPath } from '../../../../../../hack/tests/fixtures/config';

// 插件 ID：后台管理接口走 /x/<plugin-id>/api/v1，对外接口走 /x/<plugin-id>/open/v1。
export const larkPushPluginId = 'linapro-lark-push';

/** 后台管理接口路径。 */
export function apiKeyApiPath(pathName: string) {
  return pluginApiPath(larkPushPluginId, pathName);
}

/** 对外自检接口路径（签名鉴权、不发送任何消息，适合验证凭据此刻是否可用）。 */
export const openStatusPath = `/x/${larkPushPluginId}/open/v1/status`;

/** 对外文本推送接口路径。 */
export const openTextPath = `/x/${larkPushPluginId}/open/v1/message/text`;

/** 待签串末行：请求体不参与签名，固定为空串的 SHA-256。 */
const emptyBodyDigest = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';

/** 后台列表 / 详情返回的凭据投影。 */
export interface ApiKeyRow {
  id: number;
  name: string;
  appKey: string;
  group: string;
  chatId: string;
  chatPushAppId: string;
  status: number;
  remark: string;
}

// adminAccessToken 从已登录管理端的 storageState 中按值形状找出 JWT。
function adminAccessToken(storageStatePath: string): string {
  const raw = readFileSync(storageStatePath, 'utf8');
  const state = JSON.parse(raw) as {
    origins?: Array<{ localStorage?: Array<{ name: string; value: string }> }>;
  };
  const entries = (state.origins ?? []).flatMap((origin) => origin.localStorage ?? []);
  for (const entry of entries) {
    if (/^[\w-]+\.[\w-]+\.[\w-]+$/u.test(entry.value)) return entry.value;
    const match = /"(eyJ[\w-]+\.[\w-]+\.[\w-]+)"/u.exec(entry.value);
    if (match?.[1]) return match[1];
  }
  throw new Error('管理端 storageState 中未找到 accessToken（请确认 globalSetup 已完成登录）');
}

/** 用已登录管理端的令牌建立后台 API 上下文（复用同一会话与权限）。 */
export async function newAdminApi(storageStatePath: string): Promise<APIRequestContext> {
  return request.newContext({
    baseURL: config.backendBaseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${adminAccessToken(storageStatePath)}` },
  });
}

/** 唯一 app_key：带时间戳与随机串，保证用例可重复执行、互不干扰。 */
export function uniqueAppKey(prefix: string) {
  return `e2e-${prefix}-${Date.now()}-${randomBytes(3).toString('hex')}`;
}

/** 读取第一个已配置的推送分组（凭据必须绑定 push_groups 中存在的分组）。 */
export async function firstPushGroup(api: APIRequestContext): Promise<string> {
  const res = await api.get(apiKeyApiPath('push-groups'));
  const body = (await res.json()) as { data?: { list?: Array<{ name: string }> } };
  const group = body.data?.list?.[0]?.name;
  if (!group) {
    throw new Error('push_groups 未配置任何分组，无法创建签名凭据');
  }
  return group;
}

/** 按 app_key 精确查找凭据（经关键字过滤后再精确匹配）。 */
export async function findApiKey(api: APIRequestContext, appKey: string): Promise<ApiKeyRow | undefined> {
  const res = await api.get(apiKeyApiPath('api-keys'), { params: { keyword: appKey, pageSize: 100 } });
  const body = (await res.json()) as { data?: { list?: ApiKeyRow[] } };
  return body.data?.list?.find((row) => row.appKey === appKey);
}

/** 经 API 直接创建凭据（测试准备用），返回 ID。 */
export async function createApiKeyViaApi(
  api: APIRequestContext,
  input: { appKey: string; group: string; name: string; secret: string },
): Promise<number> {
  const res = await api.post(apiKeyApiPath('api-keys'), { data: { ...input, remark: 'e2e', status: 1 } });
  const body = (await res.json()) as { code: number; data?: { id: number }; message: string };
  if (body.code !== 0 || !body.data) {
    throw new Error(`创建凭据失败：${body.message}`);
  }
  return body.data.id;
}

/** 直接调用新增接口并返回统一结构的 code / errorCode，用于验证接口侧的参数校验。 */
export async function postApiKeyRaw(
  api: APIRequestContext,
  data: Record<string, unknown>,
): Promise<{ code: number; errorCode: string }> {
  const res = await api.post(apiKeyApiPath('api-keys'), { data });
  const body = (await res.json()) as { code: number; errorCode?: string };
  return { code: body.code, errorCode: body.errorCode ?? '' };
}

/** 读取凭据 secret 明文。 */
export async function readSecret(api: APIRequestContext, id: number): Promise<string> {
  const res = await api.get(apiKeyApiPath(`api-keys/${id}/secret`));
  const body = (await res.json()) as { data?: { secret: string } };
  return body.data?.secret ?? '';
}

/** 清理：删除本用例创建的凭据（不存在时忽略）。 */
export async function deleteApiKeyIfExists(api: APIRequestContext, appKey: string) {
  const row = await findApiKey(api, appKey);
  if (row) {
    await api.delete(apiKeyApiPath(`api-keys/${row.id}`));
  }
}

/** 对外接口的统一响应结构（与后台接口一致，FB-12）：code=0 为成功，errorCode 为稳定错误码。 */
interface OpenEnvelope<T> {
  code: number;
  data: null | T;
  errorCode?: string;
  message: string;
}

/** 对外接口一次调用的结果：HTTP 状态码 + 统一响应中的 code / errorCode。 */
export interface SignedCallResult {
  /** HTTP 状态码：除请求体超限（413）与服务端问题（5xx）外均为 200。 */
  status: number;
  /** 统一响应 code，0 表示成功。 */
  code: number;
  /** 稳定错误码，成功时为空串。 */
  errorCode: string;
}

/** 解析统一响应；响应体不是统一结构时抛错，避免把格式问题误判成业务结果。 */
async function readEnvelope<T>(res: { json: () => Promise<unknown>; status: () => number }) {
  const body = (await res.json()) as OpenEnvelope<T>;
  if (typeof body?.code !== 'number') {
    throw new Error(`对外接口响应不是统一结构：HTTP ${res.status()} ${JSON.stringify(body)}`);
  }
  return body;
}

/**
 * 以 HMAC 签名调用对外 status 接口，返回 HTTP 状态码与统一响应的 code / errorCode。
 * status 接口不发送任何消息，用来验证「凭据此刻能否通过鉴权」这一业务结果。
 */
export async function signedStatus(appKey: string, secret: string): Promise<SignedCallResult> {
  const ctx = await request.newContext({ baseURL: config.backendBaseURL });
  try {
    const res = await ctx.get(openStatusPath, { headers: signHeaders('GET', openStatusPath, appKey, secret) });
    const body = await readEnvelope(res);
    return { code: body.code, errorCode: body.errorCode ?? '', status: res.status() };
  } finally {
    await ctx.dispose();
  }
}

/** 计算签名请求头（请求体不参与签名，待签串末行固定为空串摘要）。 */
function signHeaders(method: string, path: string, appKey: string, secret: string): Record<string, string> {
  const timestamp = `${Math.floor(Date.now() / 1000)}`;
  const nonce = randomBytes(16).toString('hex');
  const toSign = [method, path, '', timestamp, nonce, emptyBodyDigest].join('\n');
  return {
    'X-App-Key': appKey,
    'X-Nonce': nonce,
    'X-Signature': createHmac('sha256', secret).update(toSign).digest('hex'),
    'X-Timestamp': timestamp,
  };
}

/** 文本推送接口的响应：投递有失败时 data.failed 带失败项，参数或鉴权错误时 data 为空。 */
export interface SignedTextResult extends SignedCallResult {
  allSucceeded: boolean;
  failed: Array<{ chat_id?: string; employee_no?: string; lark_code: number; lark_msg: string; lark_req_id: string }>;
}

/**
 * 以 HMAC 签名调用对外文本推送接口。用例只发往不存在的群（飞书侧拒收），不会触达真实的人或群，
 * 用来验证「服务端按凭据配置的群聊投递」这一业务结果。
 */
export async function signedText(appKey: string, secret: string, body: Record<string, unknown>): Promise<SignedTextResult> {
  const ctx = await request.newContext({ baseURL: config.backendBaseURL });
  try {
    const res = await ctx.post(openTextPath, {
      data: body,
      headers: { ...signHeaders('POST', openTextPath, appKey, secret), 'Content-Type': 'application/json' },
    });
    const envelope = await readEnvelope<{ all_succeeded: boolean; failed?: SignedTextResult['failed'] }>(res);
    return {
      allSucceeded: envelope.data?.all_succeeded ?? false,
      code: envelope.code,
      errorCode: envelope.errorCode ?? '',
      failed: envelope.data?.failed ?? [],
      status: res.status(),
    };
  } finally {
    await ctx.dispose();
  }
}

/** 测试用群聊 chat_id：符合 oc_ 格式但不存在，飞书侧会拒收，不会向真实群发消息。 */
export function fakeChatId() {
  return `oc_e2e${randomBytes(8).toString('hex')}`;
}
