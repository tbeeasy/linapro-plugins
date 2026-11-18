import { pluginApiPath, requestClient } from '#/api/request';

const pluginID = 'linapro-monitor-apilog';

function apiLogApi(pathName: string) {
  return pluginApiPath(pluginID, pathName);
}

/** 列表行。刻意不含请求头、请求体与响应体：它们需要详情权限。 */
export interface ApiLog {
  id: number;
  /** 请求开始时间（Unix 毫秒），也是分区键。 */
  requestAt: number;
  requestId: string;
  routeOwner: string;
  method: string;
  path: string;
  routePath: string;
  query: string;
  clientIp: string;
  userAgent: string;
  callerKind: string;
  callerId: string;
  callerTenant: number;
  callerName: string;
  callerNo: string;
  statusCode: number;
  costMs: number;
  errorMsg: string;
}

/** 详情行，追加已脱敏的载荷。 */
export interface ApiLogDetail extends ApiLog {
  reqHeaders: string;
  reqBody: string;
  respBody: string;
}

export interface ApiLogListParams {
  pageNum?: number;
  pageSize?: number;
  beginTime: string;
  endTime: string;
  routeOwner?: string;
  path?: string;
  callerNo?: string;
  callerName?: string;
  callerId?: string;
  statusCode?: number;
  statusType?: string;
  requestId?: string;
}

export interface ApiLogListResult {
  items: ApiLog[];
  total: number;
}

export async function apiLogList(params: ApiLogListParams) {
  return await requestClient.get<ApiLogListResult>(apiLogApi('apilog/records'), {
    params,
  });
}

/**
 * 详情查询必须带 requestAt：记录表按 request_at 分区，只给 id 会扫描全部分区。
 * 列表行本来就带 requestAt，点开详情时原样回传即可。
 */
export function apiLogDetail(id: number, requestAt: number) {
  return requestClient.get<ApiLogDetail>(apiLogApi(`apilog/records/${id}`), {
    params: { requestAt },
  });
}
