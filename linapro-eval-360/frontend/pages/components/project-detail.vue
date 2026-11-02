<script setup lang="ts">
// project-detail.vue 360 测评项目详情：
// 顶部概览卡（起止时间 / 状态 / 4 项统计合并为一张卡）→ 问卷分页列表（VxeGrid）。
// 问卷列表走后端分页接口（questionnairePage），列表行直接承载维护操作
// （进入问卷/进入关系/重命名/发布-取消/复制/删除）；顶部统计卡仍读项目详情的全量聚合。
// 「发送全部问卷」与「新增问卷」并列放在列表工具栏。
import { computed, onMounted, ref } from 'vue';

import { preferences } from '@vben/preferences';
import { Card, Divider, Input, message, Modal, Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { $t } from '#/locales';
import { formatTimestamp } from '#/utils/time';
import {
  projectDetail,
  type ProjectDetail,
  questionnaireCopy,
  questionnaireCreate,
  questionnaireDelete,
  questionnairePage,
  questionnairePublish,
  questionnaireUnpublish,
  questionnaireUpdate,
  projectSend,
  type QuestionnaireCard,
} from '../eval-client';
import { notifySendResult } from '../send-result';

const props = defineProps<{ projectId: number }>();
const emit = defineEmits<{
  back: [];
  openQuestionnaire: [
    questionnaireId: number,
    tab?: 'questionnaire' | 'relation',
  ];
}>();

const detail = ref<ProjectDetail | null>(null);
const sending = ref(false);

// 刷新项目详情：顶部信息卡 + 统计卡（跨问卷总完成率 + 参与人数）所需数据。
// 问卷列表本身由下方 VxeGrid 独立分页拉取，不依赖此处。
async function refresh() {
  const res = await projectDetail(props.projectId);
  detail.value = res.detail;
}

onMounted(refresh);

// 统计聚合：测评关系(应填) / 待填写 / 已填写 / 参与人数(各问卷被评估人之和)。
const stats = computed(() => {
  const d = detail.value;
  const expected = d?.totalExpected ?? 0;
  const completed = d?.totalCompleted ?? 0;
  const participants = (d?.questionnaires ?? []).reduce(
    (sum, q) => sum + (q.evaluateeCount ?? 0),
    0,
  );
  return {
    relations: expected,
    pending: Math.max(expected - completed, 0),
    filled: completed,
    participants,
  };
});

// statItems 供概览卡横向渲染 4 项统计（标签/数值/配色）。
const statItems = computed(() => [
  {
    key: 'relations',
    label: $t('plugin.linapro-eval-360.page.project.stats.relations'),
    value: stats.value.relations,
    color: 'text-blue-600',
  },
  {
    key: 'pending',
    label: $t('plugin.linapro-eval-360.page.project.stats.pending'),
    value: stats.value.pending,
    color: 'text-orange-500',
  },
  {
    key: 'filled',
    label: $t('plugin.linapro-eval-360.page.project.stats.filled'),
    value: stats.value.filled,
    color: 'text-green-600',
  },
  {
    key: 'participants',
    label: $t('plugin.linapro-eval-360.page.project.participants'),
    value: stats.value.participants,
    color: '',
  },
]);

// fmtDate 把 Unix 毫秒时间戳格式化为本地日期（YYYY-MM-DD）。
function fmtDate(ms: null | number) {
  if (!ms) return '-';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '-';
  const y = d.getFullYear();
  const m = `${d.getMonth() + 1}`.padStart(2, '0');
  const day = `${d.getDate()}`.padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function statusColor(status?: string) {
  if (status === 'active') return 'processing';
  if (status === 'closed') return 'default';
  return 'warning';
}

function statusLabel(status?: string) {
  return status
    ? $t(`plugin.linapro-eval-360.page.project.status.${status}`)
    : '';
}

function qPercent(q: { completed: number; expected: number }) {
  if (!q.expected || q.expected <= 0) return 0;
  return Math.round((q.completed / q.expected) * 100);
}

// ---- 问卷分页列表（VxeGrid）----

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    // 按问卷标题搜索：vben 列表插件的「搜索 + 提交」表单形态。
    schema: [
      {
        component: 'Input',
        fieldName: 'keyword',
        label: $t('plugin.linapro-eval-360.page.questionnaire.colTitle'),
        componentProps: { allowClear: true },
      },
    ],
    wrapperClass: 'grid-cols-1 md:grid-cols-2 lg:grid-cols-3',
  },
  gridOptions: {
    columns: [
      {
        field: 'title',
        title: $t('plugin.linapro-eval-360.page.questionnaire.colTitle'),
        minWidth: 220,
        align: 'left',
        slots: { default: 'title' },
      },
      {
        field: 'status',
        title: $t('plugin.linapro-eval-360.page.questionnaire.colStatus'),
        width: 100,
        slots: { default: 'status' },
      },
      {
        field: 'evaluateeCount',
        title: $t('plugin.linapro-eval-360.page.questionnaire.colEvaluatee'),
        width: 110,
      },
      {
        field: 'progress',
        title: $t('plugin.linapro-eval-360.page.questionnaire.colProgress'),
        width: 100,
        slots: { default: 'progress' },
      },
      {
        field: 'createdAt',
        title: $t('plugin.linapro-eval-360.page.questionnaire.colCreatedAt'),
        width: 170,
        formatter: ({ cellValue }: { cellValue?: null | number | string }) =>
          formatTimestamp(cellValue),
      },
      {
        field: 'action',
        title: $t('plugin.linapro-eval-360.page.questionnaire.colActions'),
        // 6 个按钮单行排布（含「取消发布」时最宽），英文文案更长需按语言放宽。
        width: preferences.app.locale === 'en-US' ? 600 : 480,
        fixed: 'right',
        align: 'left',
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    keepSource: true,
    pagerConfig: {},
    proxyConfig: {
      ajax: {
        query: async (
          { page }: { page: { currentPage: number; pageSize: number } },
          formValues: Record<string, any> = {},
        ) => {
          return await questionnairePage({
            projectId: props.projectId,
            pageNum: page.currentPage,
            pageSize: page.pageSize,
            ...formValues,
          });
        },
      },
    },
    rowConfig: { keyField: 'id' },
    id: 'eval360-questionnaire-list',
  },
  gridEvents: {
    // 点击行（操作列除外）进入问卷详情，默认落在评价关系 tab（与原卡片整卡点击一致）。
    cellClick: ({ column, row }: { column: any; row: QuestionnaireCard }) => {
      if (column.field === 'action') return;
      emit('openQuestionnaire', row.id);
    },
  },
});

// reload 同时刷新问卷分页表格与顶部统计卡（任一维护操作后调用）。
async function reload() {
  await Promise.all([gridApi.query(), refresh()]);
}

// ---- 新建问卷 ----

const createOpen = ref(false);
const createTitle = ref('');

function openCreate() {
  createTitle.value = '';
  createOpen.value = true;
}

async function confirmCreate() {
  const title = createTitle.value.trim();
  if (!title) {
    message.warning($t('plugin.linapro-eval-360.page.questionnaire.nameRequired'));
    return;
  }
  try {
    const { id } = await questionnaireCreate({
      projectId: props.projectId,
      title,
    });
    message.success($t('plugin.linapro-eval-360.page.common.createSuccess'));
    createOpen.value = false;
    // 新问卷为空壳，建完直接进入结构 tab 配维度/题目。
    emit('openQuestionnaire', id, 'questionnaire');
  } catch {
    // 重名/非法名称的中文提示由请求拦截器统一处理，弹窗保持打开供用户改名。
  }
}

// ---- 重命名问卷（弹窗） ----

const renameState = ref<null | { id: number }>(null);
const renameTitle = ref('');

function startRename(q: QuestionnaireCard) {
  renameState.value = { id: q.id };
  renameTitle.value = q.title;
}

async function confirmRename() {
  const st = renameState.value;
  if (!st) return;
  const title = renameTitle.value.trim();
  if (!title) {
    message.warning($t('plugin.linapro-eval-360.page.questionnaire.nameRequired'));
    return;
  }
  try {
    await questionnaireUpdate(st.id, { title });
    message.success($t('plugin.linapro-eval-360.page.common.updateSuccess'));
    renameState.value = null;
    await reload();
  } catch {
    // 重名提示由请求拦截器统一处理，弹窗保持打开。
  }
}

// ---- 发布/取消发布 ----

function handlePublishToggle(q: QuestionnaireCard) {
  if (q.published !== 1) {
    // 发布会立即向评价者推送卡片，先二次确认。
    Modal.confirm({
      title: $t('plugin.linapro-eval-360.page.questionnaire.publishConfirmTitle'),
      content: $t(
        'plugin.linapro-eval-360.page.questionnaire.publishConfirmContent',
      ),
      onOk: () => doPublish(q),
    });
    return;
  }
  // 取消发布会让评价者无法继续作答，先二次确认。
  Modal.confirm({
    title: $t('plugin.linapro-eval-360.page.questionnaire.unpublishConfirmTitle'),
    content: $t(
      'plugin.linapro-eval-360.page.questionnaire.unpublishConfirmContent',
    ),
    okType: 'danger',
    onOk: () => doUnpublish(q),
  });
}

async function doUnpublish(q: QuestionnaireCard) {
  try {
    await questionnaireUnpublish(q.id);
    message.success(
      $t('plugin.linapro-eval-360.page.questionnaire.unpublishSuccess'),
    );
    await reload();
  } catch {
    // 失败的中文提示由请求拦截器统一处理。
  }
}

async function doPublish(q: QuestionnaireCard) {
  try {
    // 发布即向评价者推送卡片，全部评价者送达才发布（同一键发送）。
    await questionnairePublish(q.id);
    message.success(
      $t('plugin.linapro-eval-360.page.questionnaire.publishSuccess'),
    );
    await reload();
  } catch {
    // 发布前校验（角色权重和=100%）与推送失败（含失败名单）的提示由请求拦截器统一处理，问卷保持未发布。
  }
}

// ---- 复制问卷 ----
// 新问卷名必须与同项目内其他问卷不同，故先弹窗让用户填名称；默认给未占用的「(副本)」名。
const copyState = ref<null | { sourceId: number; sourceTitle: string }>(null);
const copyTitle = ref('');

// suggestCopyTitle 基于项目详情的全量问卷名生成一个未被占用的建议名（最终以后端查重为准）。
function suggestCopyTitle(sourceTitle: string): string {
  const taken = new Set(
    (detail.value?.questionnaires ?? []).map((q) => q.title.trim()),
  );
  for (let n = 1; n < 100; n += 1) {
    const suffix = n === 1 ? '(副本)' : `(副本 ${n})`;
    const candidate = `${sourceTitle} ${suffix}`;
    if (!taken.has(candidate)) return candidate;
  }
  return `${sourceTitle} (副本)`;
}

function handleCopy(q: QuestionnaireCard) {
  copyState.value = { sourceId: q.id, sourceTitle: q.title };
  copyTitle.value = suggestCopyTitle(q.title);
}

async function confirmCopy() {
  const state = copyState.value;
  if (!state) return;
  const title = copyTitle.value.trim();
  if (!title) {
    message.warning($t('plugin.linapro-eval-360.page.questionnaire.copyNameRequired'));
    return;
  }
  try {
    await questionnaireCopy(state.sourceId, title);
    message.success($t('plugin.linapro-eval-360.page.questionnaire.copySuccess'));
    copyState.value = null;
    await reload();
  } catch {
    // 重名/非法名称的中文提示由请求拦截器统一处理，弹窗保持打开供用户改名。
  }
}

// ---- 删除问卷 ----

function handleDelete(q: QuestionnaireCard) {
  Modal.confirm({
    title: $t('plugin.linapro-eval-360.page.questionnaire.deleteConfirmTitle'),
    content: $t(
      'plugin.linapro-eval-360.page.questionnaire.deleteConfirmContent',
    ),
    okType: 'danger',
    onOk: async () => {
      await questionnaireDelete(q.id);
      message.success($t('plugin.linapro-eval-360.page.common.deleteSuccess'));
      await reload();
    },
  });
}

// ---- 发送问卷 ----

async function handleSend() {
  sending.value = true;
  try {
    // 按问卷判定发布：推送失败的问卷保持未发布，由结果提示逐份列出；请求级失败由请求拦截器提示。
    notifySendResult(await projectSend(props.projectId));
    await reload();
  } finally {
    sending.value = false;
  }
}
</script>

<template>
  <div class="flex h-full flex-col gap-4" data-testid="eval360-project-detail-panel">
    <!-- 面包屑导航：项目列表 / 当前项目名。 -->
    <div class="flex items-center gap-2 text-sm">
      <a
        class="cursor-pointer text-gray-500 hover:text-primary"
        @click="emit('back')"
      >
        {{ $t('plugin.linapro-eval-360.page.common.projectList') }}
      </a>
      <span class="text-gray-400">/</span>
      <span class="text-base font-semibold">{{ detail?.name }}</span>
    </div>

    <!-- 概览卡：起止时间 + 状态 + 4 项统计，合并成一张横向卡片，用竖分隔线区隔。
         卡片下方已无其他内容，故各块 flex-1 等宽平铺展开，填满整行。 -->
    <Card :bordered="false" class="shadow-sm">
      <div class="flex flex-nowrap items-center gap-x-4 overflow-x-auto">
        <div class="flex flex-1 flex-col items-center gap-1">
          <span class="text-sm text-gray-500">
            {{ $t('plugin.linapro-eval-360.page.project.dateRange') }}
          </span>
          <span class="font-medium whitespace-nowrap">
            {{ fmtDate(detail?.startAt ?? null) }} — {{ fmtDate(detail?.endAt ?? null) }}
          </span>
        </div>

        <Divider type="vertical" class="!mx-0 !h-10" />

        <div class="flex flex-1 flex-col items-center gap-1">
          <span class="text-sm text-gray-500">
            {{ $t('plugin.linapro-eval-360.page.project.fields.status') }}
          </span>
          <Tag :color="statusColor(detail?.status)" class="w-fit">
            {{ statusLabel(detail?.status) }}
          </Tag>
        </div>

        <!-- 4 项统计：各自 flex-1 等宽平铺，标签在数值上方，与左侧信息块同一节奏。 -->
        <template v-for="item in statItems" :key="item.key">
          <Divider type="vertical" class="!mx-0 !h-10" />
          <div class="flex flex-1 flex-col items-center gap-1">
            <span class="text-sm text-gray-500">{{ item.label }}</span>
            <span class="text-2xl font-semibold" :class="item.color">
              {{ item.value }}
            </span>
          </div>
        </template>
      </div>
    </Card>

    <!-- 问卷分页列表：占据剩余高度，内部滚动与分页由 VxeGrid 承载。 -->
    <div class="min-h-0 flex-1">
      <Grid
        :table-title="$t('plugin.linapro-eval-360.page.project.questionnaires')"
      >
        <template #toolbar-tools>
          <!-- 两个按钮之间留出间距，避免连在一起。 -->
          <div class="flex items-center gap-3">
            <a-button
              type="primary"
              :loading="sending"
              data-testid="eval360-project-send"
              @click="handleSend"
            >
              {{ $t('plugin.linapro-eval-360.page.project.sendAll') }}
            </a-button>
            <a-button
              type="primary"
              data-testid="eval360-questionnaire-create"
              @click="openCreate"
            >
              {{ $t('plugin.linapro-eval-360.page.questionnaire.create') }}
            </a-button>
          </div>
        </template>

        <!-- 问卷名称；带 testid 供 E2E 按名定位并点击进入。 -->
        <template #title="{ row }">
          <span
            class="cursor-pointer font-medium hover:text-primary"
            data-testid="eval360-questionnaire-card"
            @click="emit('openQuestionnaire', row.id)"
          >
            {{ row.title }}
          </span>
        </template>

        <!-- 发布状态独立成列。 -->
        <template #status="{ row }">
          <Tag v-if="row.published === 1" color="success">
            {{ $t('plugin.linapro-eval-360.page.questionnaire.published') }}
          </Tag>
          <Tag v-else color="default">
            {{ $t('plugin.linapro-eval-360.page.questionnaire.draft') }}
          </Tag>
        </template>

        <template #progress="{ row }">{{ qPercent(row) }}%</template>

        <template #action="{ row }">
          <div class="flex flex-nowrap items-center gap-1.5">
            <a-button
              size="small"
              type="primary"
              ghost
              data-testid="eval360-questionnaire-open"
              @click="emit('openQuestionnaire', row.id, 'questionnaire')"
            >
              {{ $t('plugin.linapro-eval-360.page.tab.questionnaire') }}
            </a-button>
            <a-button
              size="small"
              @click="emit('openQuestionnaire', row.id, 'relation')"
            >
              {{ $t('plugin.linapro-eval-360.page.tab.relation') }}
            </a-button>
            <a-button size="small" @click="startRename(row)">
              {{ $t('plugin.linapro-eval-360.page.common.rename') }}
            </a-button>
            <a-button
              size="small"
              :type="row.published === 1 ? 'default' : 'primary'"
              @click="handlePublishToggle(row)"
            >
              {{
                row.published === 1
                  ? $t('plugin.linapro-eval-360.page.questionnaire.unpublish')
                  : $t('plugin.linapro-eval-360.page.questionnaire.publish')
              }}
            </a-button>
            <a-button size="small" @click="handleCopy(row)">
              {{ $t('plugin.linapro-eval-360.page.questionnaire.copy') }}
            </a-button>
            <a-button size="small" danger @click="handleDelete(row)">
              {{ $t('plugin.linapro-eval-360.page.common.delete') }}
            </a-button>
          </div>
        </template>
      </Grid>
    </div>

    <!-- 新建问卷弹窗：仅需标题，建完进入结构编辑。 -->
    <Modal
      :open="createOpen"
      :title="$t('plugin.linapro-eval-360.page.questionnaire.createTitle')"
      :ok-text="$t('plugin.linapro-eval-360.page.common.confirm')"
      :cancel-text="$t('plugin.linapro-eval-360.page.common.cancel')"
      @ok="confirmCreate"
      @cancel="createOpen = false"
    >
      <Input
        v-model:value="createTitle"
        data-testid="eval360-questionnaire-create-title"
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.titlePlaceholder')"
        :maxlength="100"
        @press-enter="confirmCreate"
      />
    </Modal>

    <!-- 重命名问卷弹窗。 -->
    <Modal
      :open="renameState !== null"
      :title="$t('plugin.linapro-eval-360.page.questionnaire.renameTitle')"
      :ok-text="$t('plugin.linapro-eval-360.page.common.confirm')"
      :cancel-text="$t('plugin.linapro-eval-360.page.common.cancel')"
      @ok="confirmRename"
      @cancel="renameState = null"
    >
      <Input
        v-model:value="renameTitle"
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.titlePlaceholder')"
        :maxlength="100"
        @press-enter="confirmRename"
      />
    </Modal>

    <!-- 复制问卷弹窗：新名称同项目内不可重名，故不沿用源标题。 -->
    <Modal
      :open="copyState !== null"
      :title="$t('plugin.linapro-eval-360.page.questionnaire.copyTitle')"
      :ok-text="$t('plugin.linapro-eval-360.page.common.confirm')"
      :cancel-text="$t('plugin.linapro-eval-360.page.common.cancel')"
      @ok="confirmCopy"
      @cancel="copyState = null"
    >
      <div class="flex flex-col gap-2">
        <span class="text-sm text-gray-500">
          {{
            $t('plugin.linapro-eval-360.page.questionnaire.copyHint', {
              source: copyState?.sourceTitle ?? '',
            })
          }}
        </span>
        <Input
          v-model:value="copyTitle"
          :placeholder="
            $t('plugin.linapro-eval-360.page.questionnaire.copyNamePlaceholder')
          "
          @press-enter="confirmCopy"
        />
      </div>
    </Modal>
  </div>
</template>
