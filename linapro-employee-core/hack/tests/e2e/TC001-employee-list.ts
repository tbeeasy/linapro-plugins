import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';
import { prepareSourcePluginsBaseline } from '../../../../../../hack/tests/fixtures/plugin';

import { EmployeeListPage } from '../pages/EmployeeListPage';
import {
  cleanupEmployees,
  employeeApiPath,
  newAdminApi,
  seedEmployee,
} from '../support/employee-core';

// TC-1 后台「通讯录管理 → 员工列表」：菜单入口、列表渲染、搜索过滤、详情抽屉与解绑。
//
// 员工行由 Moka 同步写入、后台没有新增接口（design D25），故种子数据直接落库。
// 断言落在业务结果：种子员工出现在列表并按姓名/工号检索命中、详情抽屉给出飞书身份、
// 解绑后列表的绑定摘要随之变化（证明写操作真的落库而不是只关掉了弹窗）。
const seedTenantId = 991001;

test.describe('TC-1 通讯录管理员工列表', () => {
  test.beforeAll(async () => {
    await prepareSourcePluginsBaseline(['linapro-employee-core']);
    cleanupEmployees(seedTenantId);
  });

  test.afterAll(() => {
    cleanupEmployees(seedTenantId);
  });

  test('TC-1a: 菜单可见，列表展示同步落库的员工，按姓名与工号检索命中', async ({
    adminPage,
  }) => {
    const stamp = Date.now();
    const bound = seedEmployee({
      tenantId: seedTenantId,
      name: `E2E 已绑定-${stamp}`,
      employeeNo: `E2E-B-${stamp}`,
    });
    const unbound = seedEmployee({
      tenantId: seedTenantId,
      name: `E2E 未绑定-${stamp}`,
      employeeNo: `E2E-U-${stamp}`,
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

    // 按姓名检索：只剩命中的那一行。
    await employees.search(bound.name);
    await expect(boundRow).toBeVisible();
    await expect(employees.row(unbound.name)).toHaveCount(0);

    // 按工号检索同样命中（关键词同时匹配姓名与工号两条路径）。
    await employees.search(bound.employeeNo);
    await expect(boundRow).toBeVisible();
  });

  test('TC-1b: 详情抽屉展示飞书身份；解绑后列表绑定状态变为未绑定', async ({
    adminPage,
  }) => {
    const stamp = Date.now();
    const employee = seedEmployee({
      tenantId: seedTenantId,
      name: `E2E 详情-${stamp}`,
      employeeNo: `E2E-D-${stamp}`,
    });

    const employees = new EmployeeListPage(adminPage);
    await employees.goto();
    await employees.search(employee.name);

    const row = employees.row(employee.name);
    await expect(row).toContainText(/已绑定/iu);

    await employees.openDetail(employee.name);
    // 抽屉给出该身份：open_id、来源标注（本企业可登录）与工号。
    await expect(employees.drawer).toContainText(employee.employeeNo);
    await expect(employees.drawer).toContainText(employee.openId);
    await expect(employees.drawer).toContainText(/本企业/iu);

    await employees.unbindIdentity(employee.openId);
    // 解绑落库：抽屉里该身份消失，列表行变为未绑定。
    await expect(employees.drawer).not.toContainText(employee.openId);
    await employees.closeDrawer();
    await expect(row).toContainText(/未绑定/iu);
  });

  test('TC-1c: 未登录请求员工列表接口被拒绝', async () => {
    const api = await newAdminApi(adminStorageStatePath);
    try {
      // 不带令牌直接请求：宿主鉴权中间件必须拒绝，而不是返回花名册。
      const anonymous = await api.get(employeeApiPath(), {
        headers: { Authorization: '' },
      });
      expect(anonymous.ok()).toBe(false);
    } finally {
      await api.dispose();
    }
  });
});
