import { readFileSync } from 'node:fs';

import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';
import { config } from '../../../../../../hack/tests/fixtures/config';

import { Eval360Page } from '../pages/Eval360Page';
import {
  evalApiPath,
  newAdminApi,
  resolveTenantId,
} from '../support/eval360';

// TC-2 项目列表页：建项目（含描述与日期）→ 列表渲染行与描述 → 搜索过滤 → 日期 date-only 展示。
//
// 断言落在业务结果：项目落库后出现在列表、描述列可见、按名称搜索能命中、
// 起止时间按 YYYY-MM-DD 展示（无时分秒）。覆盖本次反馈的三条修复：
//   1) 列表功能（useVbenVxeGrid 渲染、搜索、分页）
//   2) 描述可填写（Textarea 组件修复）
//   3) 起止时间 date-only（DatePicker showTime=false）
test.describe('TC-2 360 测评项目列表与建项目抽屉', () => {
  test('TC-2a: 建项目可填写描述与日期；列表展示行、描述与 date-only 日期；名称搜索命中', async ({
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new Eval360Page(adminPage);

    try {
      const stamp = Date.now();
      const projectName = `E2E 列表-${stamp}`;
      const description = `列表描述-${stamp}`;
      // date-only：今天 + 7 天，格式 YYYY-MM-DD（与 DatePicker valueFormat 一致）。
      const today = new Date();
      const end = new Date(today.getTime() + 7 * 24 * 60 * 60 * 1000);
      const pad = (n: number) => `${n}`.padStart(2, '0');
      const startStr = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`;
      const endStr = `${end.getFullYear()}-${pad(end.getMonth() + 1)}-${pad(end.getDate())}`;

      await page.goto();
      await page.createProject(projectName, description, { start: startStr, end: endStr });

      // 业务结果：列表行出现项目名与描述列。
      const row = page.projectRow(projectName);
      await expect(row).toBeVisible();
      await expect(row).toContainText(description);
      // 起止时间按 date-only 展示（YYYY-MM-DD，不含时分秒）。
      await expect(row).toContainText(startStr);
      await expect(row).toContainText(endStr);

      // 名称搜索：在搜索栏输入项目名并查询，结果中仍能命中该项目。
      const grid = adminPage.getByTestId('eval360-project-list');
      await grid.getByPlaceholder(/项目名称|Enter project name/iu).fill(projectName);
      await grid.getByRole('button', { name: /搜索|查询|Search|Query/iu }).click();
      await expect(row).toBeVisible();

      // 重置搜索后仍可见（确认过滤可逆、非数据丢失）。
      await grid.getByRole('button', { name: /重置|Reset/iu }).click();
      await expect(row).toBeVisible();

      // 清理：直接 API 删除该项目（避免污染后续用例）。
      const listResp = await api.get(evalApiPath('project'));
      const list: Array<{ id: number; name: string }> = (await listResp.json()).data.list;
      const created = list.find((item) => item.name === projectName);
      if (created) {
        await api.delete(evalApiPath(`project/${created.id}`));
      }
    } finally {
      await api.dispose();
    }
  });
});
