import { expect, test } from "../../../../../../hack/tests/fixtures/auth";
import { adminStorageStatePath } from "../../../../../../hack/tests/fixtures/auth-state";
import { prepareSourcePluginsBaseline } from "../../../../../../hack/tests/fixtures/plugin";
import { LoginPage } from "../../../../../../hack/tests/pages/LoginPage";

import { EmployeeListPage } from "../pages/EmployeeListPage";
import {
  captureScreenshot,
  cleanupEmployees,
  cleanupRoleAndUser,
  createLimitedRole,
  createLimitedUser,
  employeeApiPath,
  employeeMenuKeys,
  menuIDsByKey,
  newAdminApi,
  seedEmployee,
} from "../support/employee-core";

// TC-1 后台「通讯录管理 → 员工列表」：菜单入口、列表渲染、搜索过滤、详情抽屉与解绑，
// 以及权限反向用例（无 edit 者看不到编辑控件、无 list 者看不到菜单）。
//
// 员工行由 Moka 同步写入、后台没有新增接口（design D25），故种子数据直接落库。
// 断言落在业务结果：种子员工出现在列表并按姓名/工号检索命中、详情抽屉给出飞书身份、
// 解绑后列表的绑定摘要随之变化（证明写操作真的落库而不是只关掉了弹窗）。
//
// 运行记录（2026-11-03，对应 tasks.md 13.14 / 14.9）：
//   环境：后端 9120（temp/bin/lina-e2e.exe，源码插件全量构建）+ vite dev 5666。
//   命令（在 hack/tests 下）：
//     E2E_PSQL_BIN=<psql> \
//     E2E_BASE_URL=http://127.0.0.1:5666 \
//     E2E_API_BASE_URL=http://127.0.0.1:9120/api/v1/ \
//     E2E_PUBLIC_BASE_URL=http://127.0.0.1:9120 \
//     E2E_FRONTEND_PROXY_BACKEND_ORIGIN=http://127.0.0.1:9120 \
//     npx playwright test --project=chromium \
//       apps/lina-plugins/linapro-employee-core/hack/tests/e2e/TC001-employee-list.ts
//   结果：5 passed (49.9s)——TC-1a / TC-1b / TC-1c / TC-1d / TC-1e 全绿。
//   多模态截图审查：temp/20261103/（列表首屏、抽屉打开、解绑后列表、两个权限拒绝场景）。
//
// 定位注意（踩过的坑，改动用例时别退回去）：
//   1. antd 给两字中文按钮自动插空格，按文案找按钮的正则必须在字间允许空白；
//   2. 操作列 fixed:"right" 使 vxe 把按钮渲染在独立容器，只能用 employee-detail-<empId>；
//   3. 抽屉从右侧滑入 500ms，截图与断言都要等动画稳定。
const seedTenantId = 991001;

// 权限反向用例的测试身份：只在本文件内创建与清理，不依赖其他用例。
const readonlyUser = {
  key: "e2e_employee_readonly",
  nickname: "E2E 员工只读用户",
  password: "e2e_pass_123",
  username: "e2e_employee_readonly",
};
const noListUser = {
  key: "e2e_employee_nolist",
  nickname: "E2E 员工无列表用户",
  password: "e2e_pass_123",
  username: "e2e_employee_nolist",
};

/** extractAccessToken 按值形状从登录响应里取 accessToken，不猜具体键位。 */
function extractAccessToken(payload: unknown): string {
  const text = JSON.stringify(payload);
  const match = /"(eyJ[\w-]+\.[\w-]+\.[\w-]+)"/u.exec(text);
  if (!match?.[1]) {
    throw new Error("登录响应中未找到 accessToken");
  }
  return match[1];
}

/** loginByPassword 用给定账号登录，返回后端 accessToken。 */
async function loginByPassword(
  username: string,
  password: string,
): Promise<string> {
  const api = await newAdminApi(adminStorageStatePath);
  try {
    const response = await api.post("/api/v1/auth/login", {
      data: { clientType: "web", password, username },
    });
    expect(
      response.ok(),
      `登录失败: ${username}, status=${response.status()}`,
    ).toBe(true);
    return extractAccessToken(await response.json());
  } finally {
    await api.dispose();
  }
}

test.describe("TC-1 通讯录管理员工列表", () => {
  test.beforeAll(async () => {
    await prepareSourcePluginsBaseline(["linapro-employee-core"]);
    cleanupEmployees(seedTenantId);
    cleanupRoleAndUser(readonlyUser.username, readonlyUser.key);
    cleanupRoleAndUser(noListUser.username, noListUser.key);
  });

  test.afterAll(() => {
    cleanupEmployees(seedTenantId);
    cleanupRoleAndUser(readonlyUser.username, readonlyUser.key);
    cleanupRoleAndUser(noListUser.username, noListUser.key);
  });

  test("TC-1a: 菜单可见，列表展示同步落库的员工，按姓名与工号检索命中", async ({
    adminPage,
  }) => {
    const stamp = Date.now();
    const bound = seedEmployee({
      tenantId: seedTenantId,
      name: `E2E 已绑定-${stamp}`,
      employeeNo: `E2E-B-${stamp}`,
      onBoardingDate: "2021-03-01",
      endProbationDate: "2021-06-01",
    });
    const unbound = seedEmployee({
      status: 0,
      tenantId: seedTenantId,
      name: `E2E 未绑定-${stamp}`,
      employeeNo: `E2E-U-${stamp}`,
      onBoardingDate: "2019-07-15",
      endProbationDate: "2019-10-15",
      leaveDate: "2024-08-31",
      withOwnIdentity: false,
    });

    const employees = new EmployeeListPage(adminPage);
    await employees.goto();
    await employees.menuVisible();

    // 列表渲染：两名种子员工都出现，绑定状态按 own 身份区分。
    const boundRow = employees.row(bound.name);
    const unboundRow = employees.row(unbound.name);
    await expect(boundRow).toContainText(bound.employeeNo);
    await expect(boundRow).toContainText(/已绑定/iu);
    await expect(unboundRow).toContainText(/未绑定/iu);

    // 三个任职日期直接随列表返回：在职员工有入职/转正、离职日期显示占位符；
    // 离职员工三个日期都照实展示（证明列表不再需要逐行打开详情补齐）。
    await expect(boundRow).toContainText(bound.onBoardingDate);
    await expect(boundRow).toContainText(bound.endProbationDate);
    await expect(boundRow).not.toContainText(/null|undefined/iu);
    await expect(unboundRow).toContainText(unbound.onBoardingDate);
    await expect(unboundRow).toContainText(unbound.endProbationDate);
    await expect(unboundRow).toContainText(unbound.leaveDate);
    await captureScreenshot(adminPage, "tc1a-list-first-screen");

    // 按姓名检索：只剩命中的那一行。
    await employees.search(bound.name);
    await expect(boundRow).toBeVisible();
    await expect(employees.row(unbound.name)).toHaveCount(0);

    // 按工号检索同样命中（关键词同时匹配姓名与工号两条路径）。
    await employees.search(bound.employeeNo);
    await expect(boundRow).toBeVisible();
  });

  test("TC-1b: 详情抽屉展示飞书身份与任职日期；解绑后列表绑定状态变为未绑定", async ({
    adminPage,
  }) => {
    const stamp = Date.now();
    const employee = seedEmployee({
      tenantId: seedTenantId,
      name: `E2E 详情-${stamp}`,
      employeeNo: `E2E-D-${stamp}`,
      onBoardingDate: "2021-03-01",
      endProbationDate: "2021-06-01",
    });

    const employees = new EmployeeListPage(adminPage);
    await employees.goto();
    await employees.search(employee.name);

    const row = employees.row(employee.name);
    await expect(row).toContainText(/已绑定/iu);

    await employees.openDetail(employee.empId);
    // 抽屉给出该身份：open_id、来源标注（本企业可登录）与工号。
    await expect(employees.drawer).toContainText(employee.employeeNo);
    await expect(employees.drawer).toContainText(employee.openId);
    await expect(employees.drawer).toContainText(/本企业/iu);
    // 任职日期来自 Moka 同步、后台只读：入职与转正日期照实展示，
    // 在职员工的离职日期为空，显示占位符而不是 "null"/"undefined"。
    await expect(employees.drawer).toContainText(employee.onBoardingDate);
    await expect(employees.drawer).toContainText(employee.endProbationDate);
    await expect(employees.drawer).not.toContainText(/null|undefined/iu);
    await captureScreenshot(adminPage, "tc1b-drawer-open");

    await employees.unbindIdentity(employee.openId);
    // 解绑落库：抽屉里该身份消失，列表行变为未绑定。
    await expect(employees.drawer).not.toContainText(employee.openId);
    await employees.closeDrawer();
    await expect(row).toContainText(/未绑定/iu);
    // 解绑后列表绑定摘要的变化也截图留证（证明落库而不只是关掉弹窗）。
    await captureScreenshot(adminPage, "tc1b-list-after-unbind");
  });

  test("TC-1c: 未登录请求员工列表接口被拒绝", async () => {
    const api = await newAdminApi(adminStorageStatePath);
    try {
      // 不带令牌直接请求：宿主鉴权中间件必须拒绝，而不是返回花名册。
      // 精确断言 401：只判 !ok() 会把 500 之类的后端故障也当成"鉴权生效"。
      const anonymous = await api.get(employeeApiPath(), {
        headers: { Authorization: "" },
      });
      expect(anonymous.status()).toBe(401);
      await expect(anonymous.text()).resolves.not.toContain('"list"');

      // 伪造令牌同样必须是 401，而不是被当成匿名放行。
      const forged = await api.get(employeeApiPath(), {
        headers: { Authorization: "Bearer forged.token.value" },
      });
      expect(forged.status()).toBe(401);
    } finally {
      await api.dispose();
    }
  });

  test("TC-1d: 无 employee:edit 权限的角色，抽屉内不出现修改/绑定/解绑控件", async ({
    page,
  }) => {
    // 只读角色：有员工列表菜单与查询权限点，但没有编辑权限点。
    const adminApi = await newAdminApi(adminStorageStatePath);
    let roleId: number;
    try {
      roleId = await createLimitedRole(adminApi, {
        key: readonlyUser.key,
        menuIds: menuIDsByKey([
          employeeMenuKeys.contacts,
          employeeMenuKeys.employees,
          employeeMenuKeys.query,
        ]),
        name: "E2E 员工只读角色",
      });
      await createLimitedUser(adminApi, {
        nickname: readonlyUser.nickname,
        password: readonlyUser.password,
        roleIds: [roleId],
        username: readonlyUser.username,
      });
    } finally {
      await adminApi.dispose();
    }

    const employee = seedEmployee({
      name: `E2E 只读-${Date.now()}`,
      employeeNo: `E2E-R-${Date.now()}`,
      tenantId: seedTenantId,
    });

    // 用只读身份登录（独立上下文，不污染 adminPage 的会话）。
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.loginAndWaitForRedirect(
      readonlyUser.username,
      readonlyUser.password,
    );

    const employees = new EmployeeListPage(page);
    await employees.goto();
    // 有 list 权限：菜单与列表都必须可用，否则下面"看不到编辑控件"的断言无意义。
    await employees.menuVisible();
    await employees.search(employee.name);
    await expect(employees.row(employee.name)).toBeVisible();

    await employees.openDetail(employee.empId);
    // 抽屉内容照常展示（只读信息可见）……
    await expect(employees.drawer).toContainText(employee.employeeNo);
    await expect(employees.drawer).toContainText(employee.openId);
    await captureScreenshot(page, "tc1d-readonly-drawer");

    // ……但三个编辑入口一个都不能出现。
    await expect(
      employees.drawer.getByRole("button", { name: /修\s*改|Edit/iu }),
    ).toHaveCount(0);
    await expect(
      employees.drawer.getByRole("button", { name: /绑定飞书身份|Bind/iu }),
    ).toHaveCount(0);
    await expect(
      employees.drawer.getByRole("button", { name: /解\s*绑|Unbind/iu }),
    ).toHaveCount(0);
    // 关联后台账号区只有展示文案，没有「修改」联动出的保存/取消。
    await expect(
      employees.drawer.getByRole("button", { name: /保\s*存|Save/iu }),
    ).toHaveCount(0);
  });

  test("TC-1e: 无 employee:list 权限的角色，侧栏看不到员工列表菜单", async ({
    page,
  }) => {
    // 该角色连通讯录管理目录都不授予：菜单不应投影到侧栏。
    const adminApi = await newAdminApi(adminStorageStatePath);
    try {
      // 只给「工作台」，保证用户能登录并渲染布局，但没有通讯录管理相关菜单。
      const roleId = await createLimitedRole(adminApi, {
        key: noListUser.key,
        menuIds: menuIDsByKey(["dashboard", "dashboard:analytics:list"]),
        name: "E2E 员工无列表角色",
      });
      await createLimitedUser(adminApi, {
        nickname: noListUser.nickname,
        password: noListUser.password,
        roleIds: [roleId],
        username: noListUser.username,
      });
    } finally {
      await adminApi.dispose();
    }

    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.loginAndWaitForRedirect(
      noListUser.username,
      noListUser.password,
    );

    const employees = new EmployeeListPage(page);
    await employees.gotoHome();
    // 侧栏本体存在（说明布局渲染完成），只是不含通讯录管理/员工列表两个条目。
    await expect(employees.sidebarMenu).toBeVisible();
    await expect(
      employees.sidebarMenu.getByText(/通讯录管理|Contacts/iu),
    ).toHaveCount(0);
    await expect(employees.sidebarMenu.getByText(/员工列表/iu)).toHaveCount(0);
    await captureScreenshot(page, "tc1e-no-list-menu");

    // 直接访问路由也不能渲染出列表表格（后端接口本身也会 403）。
    await page.goto("/contacts/employees", { waitUntil: "domcontentloaded" });
    await expect(employees.grid).toHaveCount(0);
  });
});
