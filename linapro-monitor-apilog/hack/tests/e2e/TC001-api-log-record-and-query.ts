import type { APIRequestContext, APIResponse } from '@playwright/test';

import { request as playwrightRequest } from '@playwright/test';

import { expect, test } from '@host-tests/fixtures/auth';
import { adminStorageStatePath } from '@host-tests/fixtures/auth-state';
import { config } from '@host-tests/fixtures/config';
import { ensureSourcePluginEnabled } from '@host-tests/fixtures/plugin';
import { LoginPage } from '@host-tests/pages/LoginPage';

import { ApiLogPage } from '../pages/ApiLogPage';
import {
  apiLogAdminPath,
  mintEmpToken,
  newAdminApi,
  newEmpApi,
  queryApiLogs,
  readEmpJwtSecret,
  recentWindow,
  resolveTestEmployee,
  screenshotPath,
} from '../support/apilog';

// TC-1 接口请求日志：自动记录、调用方标识、脱敏、排除名单与权限边界。
//
// 断言落在业务结果而不是页面元素：
//   - TC-1a 用真实员工 JWT 调员工端接口，断言记录被写入、caller_* 取自写入快照、
//     令牌头被脱敏、响应体与实际返回一致；
//   - TC-1b 断言默认排除名单生效（linapro-lark-push 不落库）；
//   - TC-1c 断言有后台身份的请求不被重复记录（那部分由操作日志负责）；
//   - TC-1d 断言只有列表权限的角色看不到详情入口、详情接口被拒绝。

// 员工端接口的真实挂载路径：须精确到插件前缀，避免宽泛匹配掩盖 404。
const evalEmpPath = '/x/linapro-eval-360/api/v1/eval/emp/whoami';
// 消息推送对外接口：在默认排除名单内。
const larkPushStatusPath = '/x/linapro-lark-push/open/v1/status';

test.describe('TC-1 接口请求日志', () => {
  test('TC-1a: 员工端请求被记录，调用方标识与脱敏正确', async ({ adminPage }) => {
    const employee = resolveTestEmployee();
    test.skip(
      employee === null,
      '缺少 E2E_APILOG_EMP_ID / E2E_APILOG_TENANT_ID，跳过员工端记录断言',
    );
    const secret = readEmpJwtSecret();
    expect(
      secret,
      '缺少员工 JWT 密钥（plugin.linapro-employee-core.emp_jwt_secret）',
    ).not.toBe('');

    await ensureSourcePluginEnabled(adminPage, 'linapro-monitor-apilog');

    const identity = employee!;
    const empToken = mintEmpToken(secret, identity.empId, identity.tenantId);
    const empApi = await newEmpApi(empToken);
    const adminApi = await newAdminApi(adminStorageStatePath);
    try {
      // 带真实员工 JWT 直接调用员工端接口：EmpAuth 通过后才会有调用方标识。
      const response = await empApi.get(evalEmpPath);
      expect(response.ok(), `员工端接口应成功，实际 ${response.status()}`).toBe(
        true,
      );
      const actualBody = (await response.json()) as { data?: unknown };

      const { beginTime, endTime } = recentWindow(10);
      const list = await queryApiLogs(adminApi, {
        beginTime,
        endTime,
        routeOwner: 'linapro-eval-360',
        callerId: String(identity.empId),
        pageNum: 1,
        pageSize: 10,
      });
      expect(list.items.length, '员工端请求应被接口请求日志记录').toBeGreaterThan(
        0,
      );

      const row = list.items[0] as Record<string, any>;
      expect(row.method).toBe('GET');
      expect(row.path).toContain('/api/v1/eval/emp/whoami');
      expect(row.statusCode).toBe(200);
      // 调用方标识来自 empcap.EmpAuth 写入的约定键。
      expect(row.callerKind).toBe('emp');
      expect(String(row.callerId)).toBe(String(identity.empId));
      expect(row.callerTenant).toBe(identity.tenantId);
      // 姓名与工号是写入时快照，员工端通过 EmpAuth 时一并写入。
      expect(String(row.callerNo ?? '')).not.toBe('');
      expect(String(row.callerName ?? '')).not.toBe('');

      const detailResponse = await adminApi.get(
        apiLogAdminPath(`apilog/records/${row.id}`),
        { params: { requestAt: row.requestAt } },
      );
      expect(detailResponse.ok()).toBe(true);
      const detail = ((await detailResponse.json()) as { data: any }).data;
      // 员工令牌头必须脱敏，其余头保留。
      expect(detail.reqHeaders).toContain('[REDACTED]');
      expect(detail.reqHeaders).not.toContain(empToken.split('.')[2]);
      // 响应体与实际返回一致（敏感字段除外）。
      expect(JSON.parse(detail.respBody).data).toEqual(actualBody.data);

      // 查询页：按工号筛选能命中这条记录，详情抽屉里请求头已脱敏。
      const apiLogPage = new ApiLogPage(adminPage);
      await apiLogPage.open();
      await apiLogPage.filterByCallerNo(String(row.callerNo));
      await expect(apiLogPage.rows.first()).toContainText(String(row.callerNo));
      await expect(apiLogPage.rows.first()).toContainText('/api/v1/eval/emp/whoami');
      await adminPage.screenshot({
        path: screenshotPath('tc1a-list-filtered-by-caller-no'),
        fullPage: true,
      });
      const drawer = await apiLogPage.openFirstDetail();
      await expect(drawer.getByTestId('apilog-detail-req-headers')).toContainText(
        '[REDACTED]',
      );
      // 抽屉有滑入动画：等内容完整进入视口再截图，避免截到半截。
      await expect(drawer.getByTestId('apilog-detail-req-headers')).toBeInViewport();
      await expect(drawer).toBeInViewport({ ratio: 0.9 });
      await adminPage.screenshot({
        path: screenshotPath('tc1a-detail-drawer'),
        fullPage: true,
      });
    } finally {
      await empApi.dispose();
      await adminApi.dispose();
    }
  });

  test('TC-1b: 被排除的插件不新增记录', async ({ request }) => {
    const adminApi = await newAdminApi(adminStorageStatePath);
    try {
      // 未签名的请求被 lark-push 以业务错误码拒绝（HTTP 200 + errorCode），但它确实经过了
      // lark-push 的路由——排除名单生效时不该留下任何记录。这正是本用例要保护的默认值。
      const larkPushResponse = await request.get(
        `${config.backendBaseURL}${larkPushStatusPath}`,
      );
      const larkPushBody = (await larkPushResponse.json()) as {
        errorCode?: string;
      };
      expect(larkPushBody.errorCode).toBe('LINAPRO_LARK_PUSH_SIGNATURE_MISSING');

      const { beginTime, endTime } = recentWindow(5);
      const list = await queryApiLogs(adminApi, {
        beginTime,
        endTime,
        routeOwner: 'linapro-lark-push',
        pageNum: 1,
        pageSize: 10,
      });
      expect(
        list.total,
        'linapro-lark-push 在默认排除名单内，不应被记录',
      ).toBe(0);
    } finally {
      await adminApi.dispose();
    }
  });

  test('TC-1c: 后台管理请求不记录', async ({ adminPage }) => {
    const adminApi = await newAdminApi(adminStorageStatePath);
    try {
      const { beginTime, endTime } = recentWindow(2);
      const filter = {
        beginTime,
        endTime,
        routeOwner: 'linapro-monitor-apilog',
        pageNum: 1,
        pageSize: 10,
      };
      const before = await queryApiLogs(adminApi, filter);

      // 管理端带着后台会话访问本插件的后台接口。
      await new ApiLogPage(adminPage).open();

      const after = await queryApiLogs(adminApi, filter);
      expect(
        after.total,
        '有后台登录身份的请求由操作日志负责，本插件不应重复记录',
      ).toBe(before.total);
    } finally {
      await adminApi.dispose();
    }
  });

  test('TC-1d: 只有列表权限的角色看不到详情入口，详情接口被拒绝', async ({ browser }) => {
    const adminApi = await newAdminHostApi();
    await cleanupListOnlyUser(adminApi);
    try {
      // 角色只授予「系统监控」目录与本插件的列表菜单，不授予 monitor:apilog:query 按钮。
      const roleResponse = await adminApi.post('role', {
        data: {
          name: listOnlyRoleName,
          key: listOnlyRoleKey,
          sort: 99,
          dataScope: 1,
          status: 1,
          remark: 'apilog list-only role for E2E',
          menuIds: await lookupListMenuIDs(adminApi),
        },
      });
      const roleId = (await expectApiData<{ id: number }>(roleResponse, '创建列表角色失败')).id;
      const userResponse = await adminApi.post('user', {
        data: {
          username: listOnlyUsername,
          password: listOnlyPassword,
          nickname: listOnlyUsername,
          roleIds: [roleId],
          status: 1,
        },
      });
      await expectApiData(userResponse, '创建列表用户失败');

      const userToken = await loginByPassword(listOnlyUsername, listOnlyPassword);
      const userApi = await playwrightRequest.newContext({
        baseURL: config.backendBaseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${userToken}` },
      });
      try {
        // 列表可查：证明拒绝详情不是因为整个插件都没授权。
        const { beginTime, endTime } = recentWindow(10);
        await queryApiLogs(userApi, { beginTime, endTime, pageNum: 1, pageSize: 1 });

        const detailResponse = await userApi.get(apiLogAdminPath('apilog/records/1'), {
          params: { requestAt: Date.now() },
        });
        const detailText = await detailResponse.text();
        const rejected =
          [401, 403].includes(detailResponse.status()) ||
          (detailResponse.ok() && JSON.parse(detailText).code !== 0);
        expect(rejected, `列表角色调用详情接口应被拒绝，实际 ${detailResponse.status()} ${detailText}`).toBe(true);
        expect(detailText).not.toContain('reqHeaders');
      } finally {
        await userApi.dispose();
      }

      // 页面：以该用户登录后，列表可见但没有任何详情入口。
      const context = await browser.newContext({ baseURL: config.baseURL });
      const page = await context.newPage();
      try {
        const loginPage = new LoginPage(page);
        await loginPage.goto();
        await loginPage.loginAndWaitForRedirect(listOnlyUsername, listOnlyPassword);
        const apiLogPage = new ApiLogPage(page);
        await apiLogPage.open();
        await expect(apiLogPage.table).toBeVisible();
        await expect(page.getByTestId('apilog-detail-open')).toHaveCount(0);
        await page.screenshot({
          path: screenshotPath('tc1d-list-only-no-detail'),
          fullPage: true,
        });
      } finally {
        await context.close();
      }
    } finally {
      await cleanupListOnlyUser(adminApi);
      await adminApi.dispose();
    }
  });
});

// ---- TC-1d 用到的列表权限用户造数与清理 ----

const listPermission = 'monitor:apilog:list';
// 用户与角色删除是软删除，(tenant_id, key) 唯一索引仍被占用：每次运行用独立后缀，
// 否则第二次运行建角色会撞唯一键。
const runSuffix = Date.now().toString(36);
const listOnlyRoleName = `apilog-e2e-list-only-${runSuffix}`;
const listOnlyRoleKey = `apilog_e2e_list_only_${runSuffix}`;
const listOnlyUsername = `apilog_e2e_${runSuffix}`;
const listOnlyPassword = 'apilog123';

type MenuNode = { id: number; parentId: number; perms?: string; children?: MenuNode[] };

/** 按权限标识在菜单树里找到列表菜单，连同它的父目录一起授权（缺父目录菜单不显示）。 */
async function lookupListMenuIDs(adminApi: APIRequestContext): Promise<number[]> {
  const data = await expectApiData<{ list: MenuNode[] }>(await adminApi.get('menu'), '查询菜单失败');
  const stack = [...(data.list ?? [])];
  while (stack.length > 0) {
    const node = stack.pop()!;
    if (node.perms === listPermission) return [node.parentId, node.id];
    stack.push(...(node.children ?? []));
  }
  throw new Error(`未找到权限为 ${listPermission} 的菜单`);
}

/** 只清理本次运行自己造的用户与角色：按带后缀的 username / role key 精确定位后走管理接口删除。 */
async function cleanupListOnlyUser(adminApi: APIRequestContext): Promise<void> {
  const users = await expectApiData<{ list?: Array<{ id: number; username: string }> }>(
    await adminApi.get('user', { params: { username: listOnlyUsername, pageNum: 1, pageSize: 100 } }),
    '查询测试用户失败',
  );
  for (const user of users.list ?? []) {
    if (user.username === listOnlyUsername) {
      await expectApiData(await adminApi.delete(`user/${user.id}`), '删除测试用户失败');
    }
  }
  const roles = await expectApiData<{ list?: Array<{ id: number; key: string }> }>(
    await adminApi.get('role', { params: { key: listOnlyRoleKey, page: 1, size: 100 } }),
    '查询测试角色失败',
  );
  for (const role of roles.list ?? []) {
    if (role.key === listOnlyRoleKey) {
      await expectApiData(await adminApi.delete(`role/${role.id}`), '删除测试角色失败');
    }
  }
}

async function newAdminHostApi(): Promise<APIRequestContext> {
  const token = await loginByPassword(config.adminUser, config.adminPass);
  return playwrightRequest.newContext({
    baseURL: config.apiBaseURL.replace(/\/?$/, '/'),
    extraHTTPHeaders: { Authorization: `Bearer ${token}` },
  });
}

async function loginByPassword(username: string, password: string): Promise<string> {
  const api = await playwrightRequest.newContext({ baseURL: config.apiBaseURL.replace(/\/?$/, '/') });
  try {
    const response = await api.post('auth/login', {
      data: { username, password, clientType: 'web' },
    });
    const data = await expectApiData<{ accessToken?: string }>(response, `登录失败: ${username}`);
    expect(data.accessToken, '登录成功后应返回 accessToken').toBeTruthy();
    return data.accessToken as string;
  } finally {
    await api.dispose();
  }
}

async function expectApiData<T = unknown>(response: APIResponse, message: string): Promise<T> {
  expect(response.ok(), `${message}, status=${response.status()}`).toBe(true);
  const payload = (await response.json()) as { code?: number; data?: T; message?: string };
  expect(payload.code, `${message}: ${payload.message ?? ''}`).toBe(0);
  return (payload.data ?? {}) as T;
}
