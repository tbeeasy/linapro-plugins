import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';

import { LarkPushApiKeyPage } from '../pages/LarkPushApiKeyPage';
import {
  createApiKeyViaApi,
  deleteApiKeyIfExists,
  fakeChatId,
  findApiKey,
  firstPushGroup,
  newAdminApi,
  postApiKeyRaw,
  readSecret,
  signedStatus,
  signedText,
  uniqueAppKey,
} from '../support/larkpush';

// TC-1 飞书推送凭据后台管理（FB-5）：签名凭据从配置文件迁到后台；群聊目标配置在凭据上（FB-10）。
//
// 断言落在业务结果：凭据落库与列表展示、新增 secret 必填（FB-13）且保存值可反复查看、停用 / 删除后
// 签名请求立即被拒（经对外 status 接口验证，不发送任何飞书消息）、编辑留空 secret 不改密钥、
// app_key 重复被拒。每个子用例自建自清，互不依赖执行顺序。
test.describe('TC-1 飞书推送凭据后台管理', () => {
  test('TC-1a: 新增凭据 secret 必填，保存预填值并弹窗展示，列表可见，查看密钥一致，签名可用', async ({
    adminPage,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new LarkPushApiKeyPage(adminPage);
    const appKey = uniqueAppKey('create');
    try {
      const group = await firstPushGroup(api);

      // 异常路径（FB-13）：接口侧 secret 为空直接拒绝，不落库。
      const rejected = await postApiKeyRaw(api, { appKey, group, name: 'E2E 空 secret', secret: '', status: 1 });
      expect(rejected.code).not.toBe(0);
      expect(await findApiKey(api, appKey)).toBeUndefined();

      // 异常路径（FB-13）：表单清空 secret 不允许提交，弹窗不关闭、不落库。
      await page.goto();
      await page.fillCreateForm({ appKey, group, name: 'E2E 新增', secret: '' });
      await page.submitModal();
      await expect(adminPage.getByText('请输入 secret，或点击「生成」').first()).toBeVisible();
      await expect(page.modal).toBeVisible();
      expect(await findApiKey(api, appKey)).toBeUndefined();
      await adminPage.keyboard.press('Escape');
      await expect(page.modal).toBeHidden();

      // 成功路径：打开表单即预填 64 位字母数字 secret，原样保存并在新增后弹窗展示。
      const prefilled = await page.fillCreateForm({ appKey, group, name: 'E2E 新增' });
      expect(prefilled).toMatch(/^[A-Za-z0-9]{64}$/u);
      await page.submitModal();
      const shown = await page.readSecretAndClose();
      expect(shown).toBe(prefilled);

      // 业务结果：落库、绑定所选分组、状态正常；列表行可见。
      const row = await findApiKey(api, appKey);
      expect(row?.group).toBe(group);
      expect(row?.status).toBe(1);
      await page.search(appKey);
      await expect(page.row(appKey)).toContainText(group);

      // 列表「查看密钥」可反复查看，值与新增时一致。
      await page.openSecret(row!.id);
      expect(await page.readSecretAndClose()).toBe(shown);

      // 以生成的 secret 签名访问对外自检接口：鉴权通过（不发送任何消息），统一结构 code=0。
      const result = await signedStatus(appKey, shown);
      expect(result).toEqual({ code: 0, errorCode: '', status: 200 });
    } finally {
      await deleteApiKeyIfExists(api, appKey);
      await api.dispose();
    }
  });

  test('TC-1b: 停用后签名请求立即被拒（app_key 未知），重新启用后恢复', async ({ adminPage }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new LarkPushApiKeyPage(adminPage);
    const appKey = uniqueAppKey('toggle');
    const secret = 'e2e-toggle-secret-0123456789';
    try {
      const id = await createApiKeyViaApi(api, { appKey, group: await firstPushGroup(api), name: 'E2E 启停', secret });
      expect((await signedStatus(appKey, secret)).code).toBe(0);

      await page.goto();
      await page.search(appKey);
      await page.toggleStatus(id);
      await expect.poll(async () => (await findApiKey(api, appKey))?.status).toBe(0);

      // 签名类错误按统一结构返回：HTTP 200，code 非 0，errorCode 指明原因（FB-12）。
      const rejected = await signedStatus(appKey, secret);
      expect(rejected.status).toBe(200);
      expect(rejected.code).not.toBe(0);
      expect(rejected.errorCode).toBe('LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY');

      await page.toggleStatus(id);
      await expect.poll(async () => (await findApiKey(api, appKey))?.status).toBe(1);
      expect((await signedStatus(appKey, secret)).code).toBe(0);
    } finally {
      await deleteApiKeyIfExists(api, appKey);
      await api.dispose();
    }
  });

  test('TC-1c: 编辑时 secret 留空保持原值，其它字段生效', async ({ adminPage }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new LarkPushApiKeyPage(adminPage);
    const appKey = uniqueAppKey('edit');
    const secret = 'e2e-edit-secret-0123456789';
    try {
      const id = await createApiKeyViaApi(api, { appKey, group: await firstPushGroup(api), name: 'E2E 编辑前', secret });
      await page.goto();
      await page.search(appKey);
      await page.openEdit(id);
      await adminPage.getByTestId('lark-push-api-key-name').fill('E2E 编辑后');
      await page.submitModal();
      await expect(page.modal).toBeHidden();

      await expect(page.row(appKey)).toContainText('E2E 编辑后');
      expect(await readSecret(api, id)).toBe(secret);
      expect((await signedStatus(appKey, secret)).code).toBe(0);
    } finally {
      await deleteApiKeyIfExists(api, appKey);
      await api.dispose();
    }
  });

  test('TC-1d: app_key 重复被拒；删除后签名请求被拒且同名可重建', async ({ adminPage }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new LarkPushApiKeyPage(adminPage);
    const appKey = uniqueAppKey('dup');
    const secret = 'e2e-dup-secret-0123456789';
    try {
      const group = await firstPushGroup(api);
      const id = await createApiKeyViaApi(api, { appKey, group, name: 'E2E 原凭据', secret });

      // 异常路径：同名 app_key 新增被拒，弹窗不关闭，库中仍只有一条。
      await page.goto();
      await page.fillCreateForm({ appKey, group, name: 'E2E 重复' });
      await page.submitModal();
      await expect(adminPage.getByText(/already exists|已存在/u).first()).toBeVisible();
      await expect(page.modal).toBeVisible();
      await adminPage.keyboard.press('Escape');

      // 删除后：签名立即被拒，同名 app_key 可以重新创建。
      await page.search(appKey);
      await page.delete(id);
      await expect(page.row(appKey)).toBeHidden();
      const rejected = await signedStatus(appKey, secret);
      expect(rejected.errorCode).toBe('LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY');

      await createApiKeyViaApi(api, { appKey, group, name: 'E2E 重建', secret: `${secret}-new` });
      expect((await findApiKey(api, appKey))?.name).toBe('E2E 重建');
    } finally {
      await deleteApiKeyIfExists(api, appKey);
      await api.dispose();
    }
  });

  test('TC-1e: 凭据配置群聊后按该群投递，调用方不能自行指定群聊，非法 chat_id 被拦截', async ({ adminPage }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const page = new LarkPushApiKeyPage(adminPage);
    const appKey = uniqueAppKey('chat');
    const secret = 'e2e-chat-secret-0123456789';
    const chatId = fakeChatId();
    try {
      const id = await createApiKeyViaApi(api, { appKey, group: await firstPushGroup(api), name: 'E2E 群聊', secret });

      // 异常路径：非 oc_ 开头的 chat_id 被表单拦截，弹窗不关闭，库中仍未配置群聊。
      await page.goto();
      await page.search(appKey);
      await page.openEdit(id);
      await page.fillChat('chat_not_oc');
      await page.submitModal();
      await expect(adminPage.getByText(/须以 oc_ 开头/u).first()).toBeVisible();
      await expect(page.modal).toBeVisible();
      expect((await findApiKey(api, appKey))?.chatId).toBe('');

      // 配置合法 chat_id：落库、列表可见。
      await page.fillChat(chatId);
      await page.submitModal();
      await expect(page.modal).toBeHidden();
      await expect(page.row(appKey)).toContainText(chatId);
      expect((await findApiKey(api, appKey))?.chatId).toBe(chatId);

      // 业务结果：只传正文，服务端按凭据配置的群聊投递。该群不存在，飞书拒收：HTTP 200 + SEND_PARTIAL_FAILED
      // （FB-12），data.failed 只带 chat_id 与飞书错误三元组（FB-11），不带推送应用等内部信息。
      const sent = await signedText(appKey, secret, { text: 'E2E 群聊配置校验（目标群不存在）' });
      expect(sent.status).toBe(200);
      expect(sent.code).not.toBe(0);
      expect(sent.errorCode).toBe('LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED');
      expect(sent.allSucceeded).toBe(false);
      expect(sent.failed.map((f) => f.chat_id)).toEqual([chatId]);
      expect(sent.failed[0].lark_code).toBeGreaterThan(0);
      expect(sent.failed[0].lark_req_id).not.toBe('');
      expect(Object.keys(sent.failed[0]).sort()).toEqual(['chat_id', 'lark_code', 'lark_msg', 'lark_req_id']);

      // 调用方不能自行指定群聊：请求体带 chats 以未知字段按参数错误拒绝（HTTP 200 + INVALID_PARAM），且不投递。
      const rejected = await signedText(appKey, secret, { chats: [{ chat_id: fakeChatId() }], text: 'x' });
      expect(rejected.status).toBe(200);
      expect(rejected.errorCode).toBe('LINAPRO_LARK_PUSH_INVALID_PARAM');
      expect(rejected.failed).toEqual([]);
    } finally {
      await deleteApiKeyIfExists(api, appKey);
      await api.dispose();
    }
  });
});
