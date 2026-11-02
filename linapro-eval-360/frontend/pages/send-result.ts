// send-result.ts 一键发送结果提示（项目列表与项目详情两处入口共用）。
// 一键发送按问卷判定发布：无失败时提示成功；有失败时以 warning 提示「已发布 N 份，M 份未发布」，
// 并逐份列出问卷名、原因与失败工号（工号过多时截断）。文案在调用时求值，不在模块顶层调用 $t。
import { h } from 'vue';

import { message } from 'ant-design-vue';

import { $t } from '#/locales';

import type { SendFailedItem, SendResult } from './eval-client';

// 每份失败问卷最多列出的工号数，超出部分以「等 N 人」概括。
const maxListedEmployeeNos = 10;

// 有失败时 warning 的停留秒数：清单较长，留足阅读时间。
const failedNoticeSeconds = 10;

function failReasonText(reason: SendFailedItem['reason']) {
  return reason === 'unreachable'
    ? $t('plugin.linapro-eval-360.page.project.sendFailReason.unreachable')
    : $t('plugin.linapro-eval-360.page.project.sendFailReason.pushFailed');
}

function employeeNosText(employeeNos: string[]) {
  const listed = employeeNos.slice(0, maxListedEmployeeNos).join(', ');
  if (employeeNos.length <= maxListedEmployeeNos) {
    return listed;
  }
  return `${listed} ${$t('plugin.linapro-eval-360.page.project.sendFailedMore', {
    count: employeeNos.length,
  })}`;
}

/** 按一键发送结果给出提示：全部发布为 success，有未发布问卷为 warning 并逐份列出原因。 */
export function notifySendResult(result: SendResult) {
  const failed = result.failed ?? [];
  if (failed.length === 0) {
    message.success($t('plugin.linapro-eval-360.page.project.sendSuccess'));
    return;
  }
  const summary = $t('plugin.linapro-eval-360.page.project.sendPartial', {
    failed: failed.length,
    published: result.publishedIds.length,
  });
  const lines = failed.map((item) =>
    h(
      'div',
      { 'data-testid': 'eval360-send-failed-item' },
      $t('plugin.linapro-eval-360.page.project.sendFailedItem', {
        employeeNos: employeeNosText(item.employeeNos),
        reason: failReasonText(item.reason),
        title: item.title,
      }),
    ),
  );
  message.warning({
    content: h('div', { class: 'text-left' }, [h('div', summary), ...lines]),
    duration: failedNoticeSeconds,
  });
}
