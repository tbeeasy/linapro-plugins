import { readFileSync } from 'node:fs';

import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';
import { config, workspacePath } from '../../../../../../hack/tests/fixtures/config';

import { Eval360Page } from '../pages/Eval360Page';
import {
  buildRelationWorkbook,
  cellTypeByHeader,
  evalApiPath,
  findQuestionnaireId,
  mintEmpToken,
  newAdminApi,
  pickActiveEmployees,
  pickDuplicateName,
  pickUnreachableEmployee,
  readEmpJwtSecret,
  readXlsxSheet,
  resolveTenantId,
  rowByHeader,
  seedEmployeeSession,
} from '../support/eval360';

// TC-1 全链路：管理端建卷 → Excel 全量导入关系 → 权重拦截 → 发布/一键发送
// → 员工填卷 → HR 导出两表。
//
// 断言只落在业务结果上（导出 Excel 的列结构、按得分仅计分题且分值为数字单元格、0 分留空、按答案评语
// 【】并入、权重与白名单拦截），不以「元素存在 / 弹窗关闭」作为验收依据。
test.describe('TC-1 360 测评全链路（建卷→导入→发布→作答→导出）', () => {
  test('TC-1a: 导入生成角色、权重不满足 100% 时发布被拦截并给出中文提示', async ({
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new Eval360Page(adminPage);

    try {
      const projectName = `E2E 权重拦截-${Date.now()}`;
      const created = await api.post(evalApiPath('project'), {
        data: { name: projectName, description: 'E2E' },
      });
      expect(created.ok()).toBeTruthy();
      const projectId = (await created.json()).data.id as number;

      const questionnaire = await api.post(evalApiPath('questionnaire'), {
        data: { projectId, title: '管理层 360 测评表' },
      });
      const questionnaireId = (await questionnaire.json()).data.id as number;

      const dimension = await api.post(evalApiPath('dimension'), {
        data: { questionnaireId, name: '工作表现', sort: 1 },
      });
      const dimensionId = (await dimension.json()).data.id as number;

      // 一道计分单选题（含允许评语的选项），保证「有题目」这一发布前置成立。
      const question = await api.post(evalApiPath('question'), {
        data: {
          questionnaireId,
          dimensionId,
          type: 'single_choice',
          title: '单选题',
          required: 1,
          sort: 1,
          options: [
            { label: '优秀', score: '5', allowText: 1, sort: 1 },
            { label: '良好', score: '4', sort: 2 },
            { label: '待改进', score: '0', sort: 3 },
          ],
        },
      });
      expect(question.ok()).toBeTruthy();

      // 四名在职员工：评价关系唯一键是 (问卷, 被评估人, 评价者)，一人一评；
      // 后两名供「行内新增角色 → 该角色单元格编辑」回归子断言使用。
      const [evaluatee, evaluator, peerA, peerB] = await pickActiveEmployees(api, 4);

      // 走真实 UI 完成 Excel 全量导入（含上传控件与 multipart 提交）。
      await page.goto();
      await page.openProject(projectName);
      await page.openQuestionnaire('管理层 360 测评表');

      // 草稿（未发布、无作答）：「编辑问卷」不受限，直接进编辑器、不弹三选一模式。
      await page.openTab('问卷');
      await page.editEntryButton.click();
      await expect(adminPage.getByTestId('eval360-questionnaire-editor')).toBeVisible();
      await expect(adminPage.getByTestId('eval360-edit-mode-next')).toBeHidden();

      await page.openTab('关系');

      const workbook = buildRelationWorkbook([
        {
          evaluatee: evaluatee!,
          evaluator: evaluator!,
          role: '管理员',
          jobTitle: '销售总监',
          department: '华东销售部',
        },
      ]);
      const importResponse = adminPage.waitForResponse(
        (res) => res.url().includes('/relation/import') && res.request().method() === 'POST',
      );
      await page.importRelations(workbook);
      const importResult = await importResponse;
      expect(importResult.ok()).toBeTruthy();
      const importBody = await importResult.json();
      expect(importBody.data.success).toBe(1);
      expect(importBody.data.fail).toBe(0);

      // FB-13：导入的被评估人职务/部门在明细表「被评估人」列之后以两列展示（表头为译文）。
      await page.closeImportModal();
      const headerTitles = (await page.relationMatrixHeaders().allInnerTexts()).map((t: string) => t.trim());
      expect(headerTitles.slice(0, 3)).toEqual(['被评估人', '被评估人职务', '被评估人部门']);
      const importedRow = page.evaluateeRow(evaluatee!.name);
      await expect(importedRow.getByTestId('eval360-relation-evaluateeJobTitle')).toHaveText('销售总监');
      await expect(importedRow.getByTestId('eval360-relation-evaluateeDepartment')).toHaveText(
        '华东销售部',
      );

      // FB-13：导出关系与导入模板同列，并回填被评估人职务/部门。
      const relationSheet = readXlsxSheet(await page.exportRelations());
      expect(relationSheet.header).toEqual([
        '被评估人姓名',
        '被评估人工号',
        '被评估人职务',
        '被评估人部门',
        '评价者姓名',
        '评价者工号',
        '角色',
      ]);
      expect(relationSheet.rows).toHaveLength(1);
      const exported = rowByHeader(relationSheet, relationSheet.rows[0]!);
      expect(exported['被评估人工号']).toBe(evaluatee!.employeeNo);
      expect(exported['被评估人职务']).toBe('销售总监');
      expect(exported['被评估人部门']).toBe('华东销售部');

      // 角色由导入生成、默认权重 0 → 权重不合规，发布必须被拦截。
      await page.openTab('问卷');
      await page.publish();
      const blockedToast = adminPage.locator('.ant-message-notice:visible');
      await expect(blockedToast).toContainText(/权重/);
      // 未发布：顶部「发放问卷」按钮保持发放态（发布成功后才翻为「取消发布」）。
      await expect(page.publishButton).toContainText(/发放问卷/);

      // 权重之和不足 100% 仍被拦截。
      await page.openTab('关系');
      await page.setRoleWeight('管理员', '60');
      await page.openTab('问卷');
      await page.publish();
      await expect(adminPage.locator('.ant-message-notice:visible')).toContainText(/100/);

      // 补足 100% 后放行：发放按钮翻为「取消发布」，且发布后不再允许全量导入。
      await page.openTab('关系');
      await page.setRoleWeight('管理员', '100');

      // FB-30：全部评价者都能收到飞书卡片才算发布成功。临时加一名收不到卡片的评价者 →
      // 发布失败、提示点名其工号，问卷保持未发布（修复前推送失败仍被置为已发布）；移除后再发布。
      const unreachable = await pickUnreachableEmployee(api);
      if (unreachable) {
        const added = await api.post(evalApiPath('relation'), {
          data: {
            questionnaireId,
            items: [{ evaluateeName: evaluatee!.name, evaluatorName: unreachable.name, roleName: '管理员' }],
          },
        });
        expect(added.ok()).toBeTruthy();
        const unreachableRelationId = (await added.json()).data.ids[0] as number;

        await page.openTab('问卷');
        await page.publish();
        const failedToast = adminPage.locator('.ant-message-notice:visible');
        await expect(failedToast).toContainText('无法接收飞书卡片，本次未发布任何问卷');
        await expect(failedToast).toContainText(unreachable.employeeNo);
        await expect(page.publishButton).toContainText(/发放问卷/);
        const afterFailed = await api.get(evalApiPath(`questionnaire/${questionnaireId}`));
        expect((await afterFailed.json()).data.detail.published).toBe(0);

        const removed = await api.delete(evalApiPath(`relation/${unreachableRelationId}`));
        expect(removed.ok()).toBeTruthy();
      } else {
        test.info().annotations.push({
          type: 'skip-sub-assertion',
          description: 'FB-30 发布失败路径未执行：未配置 E2E_EVAL360_UNREACHABLE_EMPLOYEE_NO',
        });
      }

      await page.openTab('问卷');
      await page.publish();
      await expect(page.publishButton).toContainText(/取消发布/);
      const afterPublished = await api.get(evalApiPath(`questionnaire/${questionnaireId}`));
      expect((await afterPublished.json()).data.detail.published).toBe(1);

      // 「编辑问卷」入口：三选一模式卡片显示译文文案，选「保留答卷」→ 下一步进入编辑器。
      await page.editEntryButton.click();
      await expect(adminPage.getByText('请选择修改问卷模式')).toBeVisible();
      await expect(adminPage.getByTestId('eval360-edit-mode-keep')).toContainText('保留答卷');
      await expect(adminPage.getByTestId('eval360-edit-mode-clear')).toContainText('删除所有答卷');
      await expect(adminPage.getByTestId('eval360-edit-mode-copy')).toContainText('复制此问卷并去编辑');
      await adminPage.getByTestId('eval360-edit-mode-keep').click();
      await adminPage.getByTestId('eval360-edit-mode-next').click();
      await expect(adminPage.getByTestId('eval360-questionnaire-editor')).toBeVisible();

      await page.openTab('关系');
      await expect(adminPage.getByTestId('eval360-relation-import-btn')).toBeDisabled();

      // FB-32：新增评价关系只录入姓名、不录入工号；弹窗中不再有工号栏。
      // 评价人姓名有两名及以上在职同名员工时整次不提交：提示点名该姓名、弹窗保持打开、明细表无新增。
      const duplicateName = pickDuplicateName();
      if (duplicateName) {
        const dupResponse = adminPage.waitForResponse(
          (res) =>
            /\/relation$/.test(new URL(res.url()).pathname) && res.request().method() === 'POST',
        );
        const dupDialog = await page.submitBatchAdd(evaluatee!.name, duplicateName, '管理员');
        await expect(dupDialog.getByTestId('eval360-batch-evaluatee-no')).toHaveCount(0);
        await expect(dupDialog.getByTestId('eval360-batch-evaluator-nos')).toHaveCount(0);
        expect((await (await dupResponse).json()).code).not.toBe(0);
        const dupToast = adminPage.locator('.ant-message-notice:visible');
        await expect(dupToast).toContainText('以下姓名存在在职同名员工，无法确定是哪一位，未提交任何评价关系');
        await expect(dupToast).toContainText(duplicateName);
        await expect(dupDialog).toBeVisible();
        await dupDialog.getByRole('button', { name: /取\s*消|Cancel/iu }).click();
        await expect(dupDialog).toBeHidden();
        await expect(await page.roleCell(evaluatee!.name, '管理员')).not.toContainText(duplicateName);
      } else {
        test.info().annotations.push({
          type: 'skip-sub-assertion',
          description: 'FB-32 重名拒绝路径未执行：未配置 E2E_EVAL360_DUPLICATE_NAME',
        });
      }

      // 回归（FB-11）：经被评估人行「新增」按名称新建的角色，紧接着在该角色单元格点「编辑」
      // 补录评价人，提交须携带该角色名（修复前编辑弹窗取不到新角色，提交报「需要输入角色」）。
      // FB-13：行内「新增」弹窗须预填该被评估人的职务/部门，并随新增请求原样提交。
      const rowAddRequest = adminPage.waitForRequest(
        (req) => /\/relation$/.test(new URL(req.url()).pathname) && req.method() === 'POST',
      );
      await page.addRelationForEvaluatee(evaluatee!.name, peerA!, '同事', {
        jobTitle: '销售总监',
        department: '华东销售部',
      });
      // FB-32：请求只带姓名，不带工号。
      const rowAddItem = (await rowAddRequest).postDataJSON().items[0];
      expect(rowAddItem.evaluateeJobTitle).toBe('销售总监');
      expect(rowAddItem.evaluateeDepartment).toBe('华东销售部');
      expect(rowAddItem.evaluatorName).toBe(peerA!.name);
      expect(rowAddItem).not.toHaveProperty('evaluatorNo');
      expect(rowAddItem).not.toHaveProperty('evaluateeNo');
      const peerCell = await page.roleCell(evaluatee!.name, '同事');
      await expect(peerCell).toContainText(`${peerA!.name}(${peerA!.employeeNo})`);
      await expect(peerCell.getByTestId('eval360-role-edit-btn')).toContainText('编辑');
      await page.openRoleEdit(evaluatee!.name, '同事');
      const editDialog = page.relationDialog();
      await expect(editDialog.getByTestId('eval360-edit-role-name')).toHaveText('同事');
      await editDialog.getByTestId('eval360-edit-add-row').click();
      await editDialog.getByTestId('eval360-edit-evaluator-name').last().fill(peerB!.name);
      const editAddRequest = adminPage.waitForRequest(
        (req) => /\/relation$/.test(new URL(req.url()).pathname) && req.method() === 'POST',
      );
      const editAddResponse = adminPage.waitForResponse(
        (res) =>
          /\/relation$/.test(new URL(res.url()).pathname) && res.request().method() === 'POST',
      );
      await editDialog.getByRole('button', { name: /确\s*定|确\s*认|OK/iu }).click();
      expect((await editAddRequest).postDataJSON().items).toEqual([
        { evaluateeName: evaluatee!.name, evaluatorName: peerB!.name, roleName: '同事' },
      ]);
      expect((await editAddResponse).ok()).toBeTruthy();
      await expect(editDialog).toBeHidden();
      await expect(await page.roleCell(evaluatee!.name, '同事')).toContainText(
        `${peerB!.name}(${peerB!.employeeNo})`,
      );
      // FB-14 回归：该单元格已有 3 名评价人，列宽（220）不足以容纳「姓名(工号)」时，
      // 必须保持一人一行并由单元格内横向滚动兜底，而不是把单条标签折成多行。
      await page.expectEvaluatorLabelsSingleLine(evaluatee!.name, '同事', [
        `${peerA!.name}(${peerA!.employeeNo})`,
        `${peerB!.name}(${peerB!.employeeNo})`,
      ]);

      // 删除角色（FB-12）：编辑弹窗右上角垃圾桶 → 二次确认（正文须为译文且点明影响范围）
      // → 该角色连同「所有被评估人」在其名下的评价关系一并消失，角色列整体从明细表移除。
      // 「同事」是本用例临时 upsert 出来的角色，只服务本断言；无作答故可删。
      await expect(page.relationMatrixHeaders().filter({ hasText: '同事' })).toHaveCount(1);
      await page.openRoleEdit(evaluatee!.name, '同事');
      await page.deleteRoleInEditModal('同事');
      await expect(page.relationMatrixHeaders().filter({ hasText: '同事' })).toHaveCount(0);

      // 删除被评估人：无作答时（TC-1a 未有员工提交）允许删除，删除后该被评估人行从明细表消失。
      // 同时断言操作栏删除按钮渲染为译文而非原始 i18n key。
      const evaluateeRow = page.evaluateeRow(evaluatee!.name);
      await expect(evaluateeRow).toHaveCount(1);
      await expect(
        evaluateeRow.getByTestId('eval360-evaluatee-delete-btn'),
      ).toContainText('删除');
      await page.deleteEvaluatee(evaluatee!.name);
      await expect(page.evaluateeRow(evaluatee!.name)).toHaveCount(0);
    } finally {
      await api.dispose();
    }
  });

  test('TC-1b: 一键发送发布问卷；员工作答后两张导出分别断言列结构与业务取值', async ({
    browser,
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new Eval360Page(adminPage);

    try {
      const projectName = `E2E 全链路-${Date.now()}`;
      const project = await api.post(evalApiPath('project'), {
        data: {
          name: projectName,
          description: 'E2E',
          endAt: Date.now() + 24 * 60 * 60 * 1000,
        },
      });
      const projectId = (await project.json()).data.id as number;

      const questionnaireResponse = await api.post(evalApiPath('questionnaire'), {
        data: { projectId, title: '管理层 360 测评表' },
      });
      const questionnaireId = (await questionnaireResponse.json()).data.id as number;
      expect(questionnaireId).toBe(await findQuestionnaireId(api, projectId));

      const dimension = await api.post(evalApiPath('dimension'), {
        data: { questionnaireId, name: '工作表现', sort: 1 },
      });
      const dimensionId = (await dimension.json()).data.id as number;

      // 0 分档即「无法评价」（问卷不设 0 分的正常选项，FB-24）。
      await api.post(evalApiPath('question'), {
        data: {
          questionnaireId,
          dimensionId,
          type: 'single_choice',
          title: '单选题',
          required: 1,
          sort: 1,
          options: [
            { label: '优秀', score: '5', allowText: 1, sort: 1 },
            { label: '良好', score: '4', sort: 2 },
            { label: '无法评价', score: '0', sort: 3 },
          ],
        },
      });
      // 矩阵题只配列（评价项）；行 = 被评估人，由评价关系动态生成，不再手填。
      await api.post(evalApiPath('question'), {
        data: {
          questionnaireId,
          dimensionId,
          type: 'matrix',
          title: '矩阵题',
          required: 1,
          sort: 2,
          matrixColumns: [
            // FB-26：首列允许附评语（非必填、无预设 → 员工端为自由输入的多行文本框），
            // 用于覆盖「评语随评价项作答保存、并按【】并入导出」的整条链路。
            {
              label: '优秀',
              score: '5',
              sort: 1,
              allowComment: 1,
              commentRequired: 0,
              commentPlaceholder: '请举例说明',
            },
            { label: '良好', score: '4', sort: 2 },
            { label: '无法评价', score: '0', sort: 3 },
          ],
        },
      });
      await api.post(evalApiPath('question'), {
        data: {
          questionnaireId,
          dimensionId,
          type: 'text_short',
          title: '一句话评价',
          required: 0,
          sort: 3,
        },
      });
      await api.post(evalApiPath('question'), {
        data: {
          questionnaireId,
          dimensionId,
          type: 'text_long',
          title: '改进建议',
          required: 0,
          sort: 4,
        },
      });

      // 同一评价者名下两名被评估人：覆盖员工端按问卷一屏批量作答。
      const [evaluatee, evaluateeB, evaluator] = await pickActiveEmployees(api, 3);

      // Excel 全量导入两条评价关系（真实的角色生成路径）。
      const workbook = buildRelationWorkbook([
        { evaluatee: evaluatee!, evaluator: evaluator!, role: '管理员' },
        { evaluatee: evaluateeB!, evaluator: evaluator!, role: '管理员' },
      ]);
      const fileBuffer = readFileSync(workbook);
      const importResponse = await api.post(evalApiPath('relation/import'), {
        params: { questionnaireId },
        multipart: {
          file: {
            name: 'relations.xlsx',
            mimeType:
              'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
            buffer: fileBuffer,
          },
        },
      });
      expect(importResponse.ok()).toBeTruthy();
      expect((await importResponse.json()).data.success).toBe(2);

      // 权重合规 → 项目级「一键发送」发布该问卷。
      const roles = await api.get(evalApiPath('relation/roles'), {
        params: { questionnaireId },
      });
      const roleList: Array<{ id: number; name: string }> = (await roles.json()).data.list;
      expect(roleList).toHaveLength(1);
      for (const role of roleList) {
        const updated = await api.put(evalApiPath(`relation/role/${role.id}/weight`), {
          data: { weight: '100' },
        });
        expect(updated.ok()).toBeTruthy();
      }

      // 受限评价者（FB-10，按评价者本人全局生效）：草稿阶段经真实编辑器把评价者加入「改进建议」
      // 的受限名单。该题选填且从不作答，导出列结构与取值断言不受影响。
      // FB-25：该题未设可见角色，受限评价者下拉须列出全部评价者（此前为空、无法选择）。
      await page.goto();
      await page.openProject(projectName);
      await page.openQuestionnaire('管理层 360 测评表');
      await page.openTab('问卷');
      await page.editEntryButton.click();
      await expect(adminPage.getByTestId('eval360-questionnaire-editor')).toBeVisible();
      await page.setExcludedEvaluators('改进建议', [
        `${evaluator!.name}（${evaluator!.employeeNo}）`,
      ]);
      // FB-19：矩阵量表分值行不显示 0 分——「无法评价」列（0 分）单元格留空，非 0 分照常显示。
      await expect(
        adminPage
          .getByTestId('eval360-question-item')
          .filter({ hasText: '矩阵题' })
          .getByTestId('eval360-question-matrix-preview')
          .getByTestId('eval360-matrix-score-cell'),
      ).toHaveText(['5', '4', '']);
      // FB-26：编辑态矩阵表每个评价项列头挂「⋮」操作栏；已开启允许评语的首列展开后
      // 同时出现四个操作（删除本列 / 允许评语 / 左移 / 右移）与三个评语子项。
      const matrixCard = adminPage.getByTestId('eval360-question-item').filter({ hasText: '矩阵题' });
      await matrixCard.getByTestId('eval360-matrix-col-menu-0').click();
      for (const id of [
        'eval360-matrix-col-delete-0',
        'eval360-matrix-col-allow-0',
        'eval360-matrix-col-required-0',
        'eval360-matrix-col-placeholder-0',
        'eval360-matrix-col-presets-0',
        'eval360-matrix-col-left-0',
        'eval360-matrix-col-right-0',
      ]) {
        await expect(adminPage.getByTestId(id)).toBeVisible();
      }
      // 首列不可左移：对应菜单项禁用。
      await expect(adminPage.getByTestId('eval360-matrix-col-left-0')).toHaveAttribute(
        'aria-disabled',
        'true',
      );
      await adminPage.keyboard.press('Escape');
      // 业务结果：问卷详情读回该题的受限名单 = 评价者本人 EmpID（而非被评估人 id）。
      const detailAfterExclude = await api.get(evalApiPath(`questionnaire/${questionnaireId}`));
      const excludedQuestion = (await detailAfterExclude.json()).data.detail.dimensions
        .flatMap((d: { questions: Array<{ title: string; excludedEvaluatorEmpIds: number[] }> }) => d.questions)
        .find((q: { title: string }) => q.title.includes('改进建议'));
      expect(excludedQuestion.excludedEvaluatorEmpIds).toEqual([evaluator!.empId]);

      await page.goto();
      await page.openProject(projectName);
      await page.sendProject();

      // 「一键发送」后问卷已发布：进入问卷详情的评测问卷子菜单，顶部发放按钮应为「取消发布」态。
      await page.openQuestionnaire('管理层 360 测评表');
      await page.openTab('问卷');
      await expect(page.publishButton).toContainText(/取消发布/);

      // ---- 员工端：真实页面作答（用 dev 密钥签发的员工 JWT 建立会话）----
      const secret = readEmpJwtSecret();
      expect(
        secret,
        '缺少员工 JWT 密钥（plugin.linapro-employee-core.emp_jwt_secret），无法建立员工端会话',
      ).not.toBe('');
      // 员工 JWT 的租户与管理端一致（同一租户才能命中该问卷数据），EmpID 来自真实员工行。
      const token = mintEmpToken(secret, evaluator!.empId, resolveTenantId(adminStorageStatePath));

      const empContext = await browser.newContext({
        baseURL: config.baseURL,
      });
      const empPage = await empContext.newPage();
      await seedEmployeeSession(empPage, token);

      // 员工端首页 = 仪表盘：最近测评任务按项目一行，进度 0/2（问卷已发布才进待办）。
      await empPage.goto(workspacePath('/eval-360/emp'), { waitUntil: 'domcontentloaded' });
      await expect(empPage.getByTestId('eval360-emp-dashboard')).toBeVisible();
      // 左侧栏两个子菜单：仪表盘（当前）/ 我的评估。
      await expect(empPage.getByTestId('emp-menu-Eval360EmpDashboard')).toHaveText('仪表盘');
      await expect(empPage.getByTestId('emp-menu-Eval360EmpTodo')).toHaveText('我的评估');
      const recentProject = empPage
        .getByTestId('eval360-emp-recent-project')
        .filter({ hasText: projectName });
      await expect(recentProject.getByTestId('eval360-emp-recent-progress')).toHaveText(
        '0 / 2 条评价',
      );
      // 角色分布必须说明视角：角色名是「我以什么身份评价对方」，不能是裸角色名（FB-27 复现场景）。
      await expect(empPage.getByText('我的评价身份')).toBeVisible();
      await expect(empPage.getByText('统计你以什么身份评价他人')).toBeVisible();
      // 按角色定位：同一评价者在其它问卷也可能有角色卡，不能假设全库只有这一张。
      await expect(
        empPage.getByTestId('eval360-emp-role-count').filter({ hasText: '管理员' }),
      ).toHaveText('作为管理员评价 2 人');

      // 点项目行 → 「我的评估」按该项目筛选：同一问卷名下两人合并为一张卡片，姓名以「、」拼接。
      await recentProject.click();
      await expect(empPage.getByTestId('eval360-emp-todo')).toBeVisible();
      await expect(empPage.getByTestId('eval360-emp-project-filter')).toHaveText(projectName);
      const assessmentCards = empPage
        .getByTestId('eval360-emp-assessment-card')
        .filter({ hasText: projectName });
      await expect(assessmentCards).toHaveCount(1);
      await expect(assessmentCards.getByTestId('eval360-emp-assessment-evaluatees')).toHaveText(
        `${evaluatee!.name}、${evaluateeB!.name}`,
      );

      await assessmentCards.first().click();
      await expect(empPage.getByTestId('eval360-emp-answer')).toBeVisible();

      // 一题一页：当前页只渲染一题；必填未答完时不出现提交按钮（漏题禁交）。
      const submitBtn = empPage.getByTestId('eval360-emp-submit');
      const nextBtn = empPage.getByTestId('eval360-emp-next');
      const questionIndex = empPage.getByTestId('eval360-emp-question-index');
      await expect(submitBtn).toHaveCount(0);

      // 按题走：每题一张「行=被评估人」表格，逐行作答。
      const questionBlock = (title: string) =>
        empPage.getByTestId('eval360-emp-question').filter({ hasText: title }).first();
      const gridRow = (title: string, name: string) =>
        questionBlock(title).getByTestId('eval360-emp-grid-row').filter({ hasText: name });

      // 受限评价者全局生效：评价者本人在名单内 → 无论评谁都看不到「改进建议」整题，
      // 翻页总数只含其余 3 题（排除只作用于该题、只作用于名单内的人）。
      await expect(questionIndex).toHaveText('第 1 / 3 题');
      await expect(empPage.getByTestId('eval360-emp-question')).toHaveCount(1);
      await expect(questionBlock('单选题')).toBeVisible();

      // 第 1 题 单选：A→优秀 + 选项级评语；B→无法评价（0 分，覆盖按得分表单选 0 分留空）。
      // 选项列顺序：优秀 / 良好 / 无法评价。
      await gridRow('单选题', evaluatee!.name)
        .getByTestId('eval360-emp-choice-cell')
        .nth(0)
        .click();
      await gridRow('单选题', evaluatee!.name)
        .getByTestId('eval360-emp-comment')
        .fill('配合度高');
      // 必答题按「题 × 未提交行」判定：只答了 A 时不能进入下一题。
      await expect(nextBtn).toBeDisabled();
      await gridRow('单选题', evaluateeB!.name)
        .getByTestId('eval360-emp-choice-cell')
        .nth(2)
        .click();

      // 本题答完可翻页；只答完一题时仍不可提交。
      await expect(submitBtn).toHaveCount(0);
      await expect(nextBtn).toBeEnabled();
      await nextBtn.click();
      await expect(questionIndex).toHaveText('第 2 / 3 题');

      // FB-19：员工端矩阵分值行同样不显示 0 分（列序：优秀 5 / 良好 4 / 无法评价 0）。
      await expect(
        questionBlock('矩阵题').getByTestId('eval360-emp-matrix-score'),
      ).toHaveText(['5', '4', '']);

      // 第 2 题 矩阵：行=被评估人，A→优秀（附评语），B→无法评价。
      const matrixRowA = gridRow('矩阵题', evaluatee!.name);
      // FB-26：未选列前不出现评语入口；选中「优秀」后单选钮旁出现评语图标，评语框以浮层自动弹出
      // （浮层 teleport 到 body，故从页面级查询），并以上配置的提示语作为 placeholder。
      await expect(matrixRowA.getByTestId('eval360-emp-matrix-comment-toggle')).toHaveCount(0);
      await matrixRowA.getByTestId('eval360-emp-matrix-cell').nth(0).click();
      await expect(matrixRowA.getByTestId('eval360-emp-matrix-comment-toggle')).toBeVisible();
      const matrixComment = empPage.getByTestId('eval360-emp-matrix-comment');
      await expect(matrixComment).toBeVisible();
      await expect(matrixComment).toHaveAttribute('placeholder', '请举例说明');
      await matrixComment.fill('带队打赢了硬仗');
      // 点浮层外收起后图标标蓝（已填评语），再去选 B 行。
      await empPage.getByTestId('eval360-emp-question-index').click();
      await expect(matrixComment).toBeHidden();
      await gridRow('矩阵题', evaluateeB!.name)
        .getByTestId('eval360-emp-matrix-cell')
        .nth(2)
        .click();

      // 必填全部答完即出现提交按钮（选填题未答不影响）。
      await expect(submitBtn).toBeVisible();
      await nextBtn.click();
      await expect(questionIndex).toHaveText('第 3 / 3 题');
      await expect(nextBtn).toHaveCount(0);

      // 第 3 题 单行文本（选填）：只给 A 填。
      await questionBlock('一句话评价')
        .locator('[data-testid="eval360-emp-text"]')
        .first()
        .fill('推进有力');

      await expect(submitBtn).toBeEnabled();
      await expect(submitBtn).toContainText('提交（2 人）');
      await submitBtn.click();
      await expect(empPage.getByTestId('eval360-emp-result')).toBeVisible();
      await expect(empPage.getByTestId('eval360-emp-result')).toContainText('已提交 2 人');

      // 一次批量提交两人：问卷卡片整体移到「已完成评估」页签。
      await empPage.goto(workspacePath('/eval-360/emp/todo'), { waitUntil: 'domcontentloaded' });
      await expect(empPage.getByTestId('eval360-emp-todo')).toBeVisible();
      await expect(
        empPage.getByTestId('eval360-emp-assessment-card').filter({ hasText: projectName }),
      ).toHaveCount(0);
      await empPage.getByTestId('eval360-emp-tab-done').click();
      const doneCard = empPage
        .getByTestId('eval360-emp-assessment-card')
        .filter({ hasText: projectName });
      await expect(doneCard).toHaveCount(1);
      // 已完成卡片仅展示：点击不进入作答页。
      await doneCard.click();
      await expect(empPage.getByTestId('eval360-emp-todo')).toBeVisible();
      await expect(empPage.getByTestId('eval360-emp-answer')).toHaveCount(0);
      await empContext.close();

      // ---- HR 导出两表并断言业务结果 ----
      // 矩阵题一题一列（行即被评估人）：一条已提交作答 = 一个被评估人一行，矩阵列取其所选评价项。
      await page.goto();
      await page.openProject(projectName);
      await page.openQuestionnaire('管理层 360 测评表');
      await page.openTab('报告');

      // FB-23：两表均不输出「评价者」列，前导列收窄为 5 列。
      const answerSheet = readXlsxSheet(await page.exportByAnswer());
      expect(answerSheet.header).toEqual([
        '序号',
        '提交时间',
        '所用时间',
        '关系角色',
        '被评价对象',
        '单选题',
        '矩阵题',
        '一句话评价',
        '改进建议',
      ]);
      expect(answerSheet.rows).toHaveLength(2);
      const answerRowOf = (name: string) =>
        answerSheet.rows
          .map((r) => rowByHeader(answerSheet, r))
          .find((r) => r['被评价对象'] === name)!;
      const answerA = answerRowOf(evaluatee!.name);
      expect(answerA['关系角色']).toBe('管理员');
      expect(answerA['评价者']).toBeUndefined();
      // 选项级评语并入该题单元格、以【】包裹；矩阵评价项评语同一口径（FB-26）。
      expect(answerA['单选题']).toBe('优秀【配合度高】');
      expect(answerA['矩阵题']).toBe('优秀【带队打赢了硬仗】');
      expect(answerA['一句话评价']).toBe('推进有力');
      expect(answerA['改进建议']).toBe('');
      const answerB = answerRowOf(evaluateeB!.name);
      expect(answerB['单选题']).toBe('无法评价');
      expect(answerB['矩阵题']).toBe('无法评价');
      expect(answerB['一句话评价']).toBe('');

      const scoreSheet = readXlsxSheet(await page.exportByScore());
      expect(scoreSheet.header).toEqual([
        '序号',
        '提交时间',
        '所用时间',
        '关系角色',
        '被评价对象',
        '单选题',
        '矩阵题',
      ]);
      expect(scoreSheet.rows).toHaveLength(2);
      const scoreRowIndexOf = (name: string) =>
        scoreSheet.rows.findIndex((r) => rowByHeader(scoreSheet, r)['被评价对象'] === name);
      const scoreRowOf = (name: string) =>
        rowByHeader(scoreSheet, scoreSheet.rows[scoreRowIndexOf(name)]!);
      expect(scoreRowOf(evaluatee!.name)['评价者']).toBeUndefined();
      // FB-24：分值写数字单元格（Excel 可直接求和/求均值），不是文本。
      expect(scoreRowOf(evaluatee!.name)['单选题']).toBe('5');
      expect(scoreRowOf(evaluatee!.name)['矩阵题']).toBe('5');
      const rowA = scoreRowIndexOf(evaluatee!.name);
      expect(cellTypeByHeader(scoreSheet, rowA, '单选题')).toBe('n');
      expect(cellTypeByHeader(scoreSheet, rowA, '矩阵题')).toBe('n');
      // FB-24：0 分即「无法评价」，单选与矩阵均为空单元格（不是 0、不是 N/A、也不是空串文本）。
      const rowB = scoreRowIndexOf(evaluateeB!.name);
      expect(scoreRowOf(evaluateeB!.name)['单选题']).toBe('');
      expect(scoreRowOf(evaluateeB!.name)['矩阵题']).toBe('');
      expect(cellTypeByHeader(scoreSheet, rowB, '单选题')).toBe('');
      expect(cellTypeByHeader(scoreSheet, rowB, '矩阵题')).toBe('');

      // 删除被评估人受作答拦截：该被评估人已有提交，删除必须被后端拒绝，且删除后仍在明细中。
      const matrixBefore = await api.get(evalApiPath('relation'), {
        params: { questionnaireId },
      });
      const targetEvaluatee = (await matrixBefore.json()).data.matrix.evaluatees.find(
        (e: { evaluateeId: number; evaluateeName: string }) =>
          e.evaluateeName === evaluatee!.name,
      );
      expect(targetEvaluatee).toBeTruthy();
      const blockedDelete = await api.delete(
        evalApiPath(`relation/evaluatee/${targetEvaluatee.evaluateeId}`),
      );
      expect((await blockedDelete.json()).message).toContain('作答');
      const matrixAfter = await api.get(evalApiPath('relation'), {
        params: { questionnaireId },
      });
      expect(
        (await matrixAfter.json()).data.matrix.evaluatees.some(
          (e: { evaluateeName: string }) => e.evaluateeName === evaluatee!.name,
        ),
      ).toBe(true);

      // 删除角色受作答拦截（FB-12）：该角色下已有提交，级联删除必须被拒绝，
      // 且拒绝后角色与其评价关系原样保留（不做部分删除）。
      const rolesBefore = (await matrixAfter.json()).data.matrix.roles as {
        id: number;
        name: string;
      }[];
      const submittedRole = rolesBefore.find((r) => r.name === '管理员');
      expect(submittedRole).toBeTruthy();
      const blockedRoleDelete = await api.delete(
        `${evalApiPath(`relation/role/${submittedRole!.id}`)}?cascade=true`,
      );
      expect((await blockedRoleDelete.json()).message).toContain('作答');
      const matrixAfterRoleDelete = await api.get(evalApiPath('relation'), {
        params: { questionnaireId },
      });
      const matrixBody = (await matrixAfterRoleDelete.json()).data.matrix;
      expect(
        (matrixBody.roles as { name: string }[]).some((r) => r.name === '管理员'),
      ).toBe(true);
      expect(
        (
          matrixBody.evaluatees as { groups: { roleName: string; items: unknown[] }[] }[]
        ).some((e) => e.groups.some((g) => g.roleName === '管理员' && g.items.length > 0)),
      ).toBe(true);
    } finally {
      await api.dispose();
    }
  });

  // FB-31：一键发送按问卷判定发布。两份问卷共享一名可达评价者，其中一份另含一名收不到飞书卡片的
  // 评价者 → 只有含不可达评价者的问卷保持未发布，另一份照常发布；请求正常返回，前端以 warning
  // 提示「已发布 1 份，1 份未发布」并列出未发布问卷名、原因与工号（修复前整个项目一份都不发布）。
  test('TC-1c: 一键发送部分发布：含不可达评价者的问卷保持未发布，其余问卷照常发布', async ({
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new Eval360Page(adminPage);

    try {
      const unreachable = await pickUnreachableEmployee(api);
      if (!unreachable) {
        test.info().annotations.push({
          type: 'skip-sub-assertion',
          description: 'FB-31 一键发送部分发布未执行：未配置 E2E_EVAL360_UNREACHABLE_EMPLOYEE_NO',
        });
        test.skip(true, '未配置 E2E_EVAL360_UNREACHABLE_EMPLOYEE_NO');
        return;
      }

      const projectName = `E2E 部分发布-${Date.now()}`;
      const project = await api.post(evalApiPath('project'), {
        data: {
          name: projectName,
          description: 'E2E',
          endAt: Date.now() + 24 * 60 * 60 * 1000,
        },
      });
      expect(project.ok()).toBeTruthy();
      const projectId = (await project.json()).data.id as number;

      const [evaluatee, evaluator] = await pickActiveEmployees(api, 2);
      const reachableTitle = '部分发布-全员可达';
      const blockedTitle = '部分发布-含不可达';
      const plans = [
        { title: reachableTitle, evaluators: [evaluator!] },
        { title: blockedTitle, evaluators: [evaluator!, unreachable] },
      ];
      const questionnaireIds: Record<string, number> = {};
      for (const plan of plans) {
        const created = await api.post(evalApiPath('questionnaire'), {
          data: { projectId, title: plan.title },
        });
        expect(created.ok()).toBeTruthy();
        const questionnaireId = (await created.json()).data.id as number;
        questionnaireIds[plan.title] = questionnaireId;

        const dimension = await api.post(evalApiPath('dimension'), {
          data: { questionnaireId, name: '工作表现', sort: 1 },
        });
        const dimensionId = (await dimension.json()).data.id as number;
        const question = await api.post(evalApiPath('question'), {
          data: {
            questionnaireId,
            dimensionId,
            type: 'single_choice',
            title: '单选题',
            required: 1,
            sort: 1,
            options: [
              { label: '优秀', score: '5', sort: 1 },
              { label: '良好', score: '4', sort: 2 },
            ],
          },
        });
        expect(question.ok()).toBeTruthy();

        const workbook = buildRelationWorkbook(
          plan.evaluators.map((ev) => ({ evaluatee: evaluatee!, evaluator: ev, role: '管理员' })),
        );
        const imported = await api.post(evalApiPath('relation/import'), {
          params: { questionnaireId },
          multipart: {
            file: {
              name: 'relations.xlsx',
              mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
              buffer: readFileSync(workbook),
            },
          },
        });
        expect(imported.ok()).toBeTruthy();
        expect((await imported.json()).data.success).toBe(plan.evaluators.length);

        // 权重合规（校验整批判定，须两份都通过才进入推送）。
        const roles = await api.get(evalApiPath('relation/roles'), { params: { questionnaireId } });
        for (const role of (await roles.json()).data.list as Array<{ id: number }>) {
          const updated = await api.put(evalApiPath(`relation/role/${role.id}/weight`), {
            data: { weight: '100' },
          });
          expect(updated.ok()).toBeTruthy();
        }
      }

      await page.goto();
      await page.openProject(projectName);
      const sendResponse = adminPage.waitForResponse(
        (res) => res.url().includes(`/project/${projectId}/send`) && res.request().method() === 'POST',
      );
      await page.sendProject();

      // 接口契约：部分失败仍正常返回，只发布可达问卷，失败清单点名不可达问卷与其不可达工号。
      const sendBody = await (await sendResponse).json();
      expect(sendBody.code).toBe(0);
      expect(sendBody.data.publishedIds).toEqual([questionnaireIds[reachableTitle]]);
      expect(sendBody.data.failed).toEqual([
        {
          questionnaireId: questionnaireIds[blockedTitle],
          title: blockedTitle,
          reason: 'unreachable',
          employeeNos: [unreachable.employeeNo],
        },
      ]);

      // 用户可见：warning 提示为中文译文（非 i18n key），逐份列出问卷名、原因与工号。
      const notice = adminPage.locator('.ant-message-notice:visible');
      await expect(notice).toContainText('已发布 1 份，1 份未发布');
      const failedItems = notice.getByTestId('eval360-send-failed-item');
      await expect(failedItems).toHaveCount(1);
      await expect(failedItems.first()).toHaveText(
        `《${blockedTitle}》有评价者无法接收飞书卡片：${unreachable.employeeNo}`,
      );

      // 业务结果：API 读回可达问卷已发布、含不可达评价者的问卷保持未发布。
      const reachableDetail = await api.get(
        evalApiPath(`questionnaire/${questionnaireIds[reachableTitle]}`),
      );
      expect((await reachableDetail.json()).data.detail.published).toBe(1);
      const blockedDetail = await api.get(
        evalApiPath(`questionnaire/${questionnaireIds[blockedTitle]}`),
      );
      expect((await blockedDetail.json()).data.detail.published).toBe(0);
    } finally {
      await api.dispose();
    }
  });
});

