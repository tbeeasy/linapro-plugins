import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';

import { Eval360Page } from '../pages/Eval360Page';
import { evalApiPath, newAdminApi } from '../support/eval360';

// TC-2 项目列表页（卡片式）：建项目（含描述与日期）→ 卡片渲染名称/状态/date-only 日期
// → 编辑抽屉回显描述与日期 → 状态分段筛选 → 卡片内删除。
//
// 断言落在业务结果：项目落库后以卡片出现在列表、状态为「草稿」（无已发布问卷）、
// 起止时间按 YYYY-MM-DD 展示（无时分秒）、描述经接口与编辑抽屉均读回原值、
// 状态筛选按项目状态命中/排除、删除后卡片消失且接口不再返回该项目。
test.describe('TC-2 360 测评项目列表与建项目抽屉', () => {
  test('TC-2a: 建项目可填写描述与日期；卡片展示状态与 date-only 日期；编辑回显；状态筛选；删除', async ({
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new Eval360Page(adminPage);
    const stamp = Date.now();
    const projectName = `E2E 列表-${stamp}`;
    const description = `列表描述-${stamp}`;

    try {
      // date-only：今天 + 7 天，格式 YYYY-MM-DD（与 DatePicker valueFormat 一致）。
      const today = new Date();
      const end = new Date(today.getTime() + 7 * 24 * 60 * 60 * 1000);
      const pad = (n: number) => `${n}`.padStart(2, '0');
      const fmt = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
      const startStr = fmt(today);
      const endStr = fmt(end);

      await page.goto();
      // 页面标题为译文而非原始 i18n key。
      await expect(adminPage.getByTestId('eval360-project-list')).toContainText('测评项目管理');
      await page.createProject(projectName, description, { start: startStr, end: endStr });

      // 业务结果：卡片展示项目名、「草稿」状态（尚无已发布问卷）与 date-only 起止区间。
      const card = page.projectCard(projectName);
      await expect(card).toContainText('草稿');
      await expect(card).toContainText(`${startStr} ~ ${endStr}`);
      await expect(card).not.toContainText(/\d{2}:\d{2}/);

      // 描述已落库：接口读回原值（卡片本身不展示描述）。
      const listResp = await api.get(evalApiPath('project'));
      expect(listResp.ok()).toBeTruthy();
      const list: Array<{ description: string; id: number; name: string; status: string }> = (
        await listResp.json()
      ).data.list;
      const created = list.find((item) => item.name === projectName);
      expect(created?.description).toBe(description);
      expect(created?.status).toBe('draft');

      // 编辑抽屉回显：描述与 date-only 日期均为创建时填写的值。
      const hovered = await page.hoverProjectCard(projectName);
      await hovered.getByRole('button', { name: /^编\s*辑$/ }).click();
      const drawer = adminPage.locator('[role="dialog"]').filter({ visible: true }).first();
      await expect(drawer).toContainText('编辑项目');
      await expect(drawer.getByLabel('描述').first()).toHaveValue(description);
      await expect(drawer.getByLabel('开始时间').first()).toHaveValue(startStr);
      await expect(drawer.getByLabel('截止时间').first()).toHaveValue(endStr);
      await adminPage.keyboard.press('Escape');
      await expect(drawer).toBeHidden();

      // 状态分段筛选：「草稿」命中、「已结束」排除、回到「全部」仍可见（过滤可逆、非数据丢失）。
      await page.filterProjectStatus('草稿');
      await expect(page.projectCard(projectName)).toBeVisible();
      await page.filterProjectStatus('已结束');
      await expect(page.projectCard(projectName)).toHaveCount(0);
      await page.filterProjectStatus('全部');
      await expect(page.projectCard(projectName)).toBeVisible();

      // 卡片内删除：二次确认后卡片消失，接口不再返回该项目。
      const toDelete = await page.hoverProjectCard(projectName);
      await toDelete.getByRole('button', { name: /^删\s*除$/ }).click();
      const popconfirm = adminPage.locator('.ant-popover:visible');
      await expect(popconfirm).toContainText('删除项目将连同其问卷、关系一并清除，确认？');
      await popconfirm.locator('.ant-btn-primary').click();
      await expect(page.projectCard(projectName)).toHaveCount(0);
      const afterResp = await api.get(evalApiPath('project'));
      const after: Array<{ name: string }> = (await afterResp.json()).data.list;
      expect(after.some((item) => item.name === projectName)).toBeFalsy();
    } finally {
      // 兜底清理：中途失败时经 API 删除残留项目，避免污染后续用例。
      const resp = await api.get(evalApiPath('project'));
      if (resp.ok()) {
        const list: Array<{ id: number; name: string }> = (await resp.json()).data.list;
        const leftover = list.find((item) => item.name === projectName);
        if (leftover) {
          await api.delete(evalApiPath(`project/${leftover.id}`));
        }
      }
      await api.dispose();
    }
  });

  // FB-3：问卷列表行内操作收纳为单个「更多操作」下拉（菜单项不带颜色），并新增「评价人数」列。
  // 业务结果落在：新列表头与取值（无评价关系时为 0，去重口径由后端集成测试覆盖）、
  // 下拉内 6 项操作为译文且无危险色、经下拉「重命名」后问卷标题在接口与列表中均已变更。
  test('TC-2b: 项目详情问卷列表显示评价人数列，行操作收纳在「更多操作」下拉且可用', async ({
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new Eval360Page(adminPage);
    const stamp = Date.now();
    const projectName = `E2E 问卷列表-${stamp}`;
    const title = `列表问卷-${stamp}`;
    const renamed = `列表问卷改名-${stamp}`;
    let projectId = 0;

    try {
      const created = await api.post(evalApiPath('project'), {
        data: { name: projectName, description: 'E2E' },
      });
      expect(created.ok()).toBeTruthy();
      projectId = (await created.json()).data.id as number;
      const questionnaire = await api.post(evalApiPath('questionnaire'), {
        data: { projectId, title },
      });
      expect(questionnaire.ok()).toBeTruthy();

      await page.goto();
      await page.openProject(projectName);

      // 新列：表头为译文，接口与界面一致（尚无评价关系 → 0）。
      const pageResp = await api.get(evalApiPath('questionnaire/page'), {
        params: { projectId, pageNum: 1, pageSize: 10 },
      });
      expect(pageResp.ok()).toBeTruthy();
      const items: Array<{ evaluatorCount: number; title: string }> = (await pageResp.json()).data
        .items;
      expect(items.find((item) => item.title === title)?.evaluatorCount).toBe(0);
      await expect(await page.questionnaireCell(title, '评价人数')).toHaveText('0');

      // 操作列只剩一个无主色/危险色的「更多操作」按钮。
      const more = await page.questionnaireMoreButton(title);
      await expect(more).toHaveText(/更多操作/);
      await expect(more).not.toHaveClass(/ant-btn-primary|ant-btn-dangerous/);

      // 下拉内 6 项操作均为译文，且没有危险色菜单项。
      const menu = await page.openQuestionnaireMoreMenu(title);
      await expect(menu.locator('.ant-dropdown-menu-item')).toHaveText([
        '评测问卷',
        '评价关系',
        '重命名',
        '发布',
        '复制问卷',
        '删除',
      ]);
      await expect(menu.locator('.ant-dropdown-menu-item-danger')).toHaveCount(0);

      // 经下拉「重命名」改名：接口与列表都读到新标题。
      await menu.getByText('重命名', { exact: true }).click();
      const dialog = adminPage.locator('.ant-modal').filter({ hasText: '重命名问卷' });
      await expect(dialog).toBeVisible();
      await dialog.locator('input').fill(renamed);
      await dialog.getByRole('button', { name: /确\s*定/ }).click();
      await expect(dialog).toBeHidden();
      await expect(page.questionnaireRow(renamed)).toBeVisible();
      const afterResp = await api.get(evalApiPath('questionnaire/page'), {
        params: { projectId, pageNum: 1, pageSize: 10 },
      });
      const after: Array<{ title: string }> = (await afterResp.json()).data.items;
      expect(after.map((item) => item.title)).toEqual([renamed]);
    } finally {
      if (projectId) {
        await api.delete(evalApiPath(`project/${projectId}`));
      }
      await api.dispose();
    }
  });
});
