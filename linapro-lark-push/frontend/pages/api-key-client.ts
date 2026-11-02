import { pluginApiPath, requestClient } from "#/api/request";

const pluginID = "linapro-lark-push";

function apiKeyApi(pathName: string) {
  return pluginApiPath(pluginID, pathName);
}

/** 签名凭据（列表 / 详情投影，不含 secret）。 */
export interface ApiKeyItem {
  id: number;
  name: string;
  appKey: string;
  group: string;
  /** 配置的群聊 chat_id；空串表示不推群聊。 */
  chatId: string;
  /** 发送群聊所用的推送应用；空串表示用分组唯一的推送应用。 */
  chatPushAppId: string;
  status: number;
  remark: string;
  createdAt: null | number;
  updatedAt: null | number;
}

export interface ApiKeyListParams {
  pageNum?: number;
  pageSize?: number;
  keyword?: string;
  group?: string;
  status?: number;
}

export interface ApiKeySaveInput {
  name: string;
  appKey: string;
  secret: string;
  group: string;
  chatId: string;
  chatPushAppId: string;
  status: number;
  remark: string;
}

/** 推送分组及其推送应用（不含密钥）。 */
export interface PushGroup {
  name: string;
  pushApps: { appId: string; name: string }[];
}

export async function listApiKeys(params?: ApiKeyListParams) {
  const res = await requestClient.get<{ list: ApiKeyItem[]; total: number }>(
    apiKeyApi("api-keys"),
    { params },
  );
  return { items: res.list, total: res.total };
}

export async function getApiKey(id: number) {
  return requestClient.get<ApiKeyItem>(apiKeyApi(`api-keys/${id}`));
}

export async function getApiKeySecret(id: number) {
  const res = await requestClient.get<{ secret: string }>(
    apiKeyApi(`api-keys/${id}/secret`),
  );
  return res.secret;
}

export async function createApiKey(values: ApiKeySaveInput) {
  return requestClient.post<{ id: number; secret: string }>(
    apiKeyApi("api-keys"),
    values,
  );
}

export async function updateApiKey(id: number, values: ApiKeySaveInput) {
  return requestClient.put(apiKeyApi(`api-keys/${id}`), values);
}

export async function updateApiKeyStatus(id: number, status: number) {
  return requestClient.put(apiKeyApi(`api-keys/${id}/status`), { status });
}

export async function deleteApiKey(id: number) {
  return requestClient.delete(apiKeyApi(`api-keys/${id}`));
}

export async function listPushGroups() {
  const res = await requestClient.get<{ list: PushGroup[] }>(
    apiKeyApi("push-groups"),
  );
  return res.list ?? [];
}
