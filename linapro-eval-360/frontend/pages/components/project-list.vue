<script setup lang="ts">
// project-list.vue 360 测评项目列表（管理端首页）——卡片式布局。
// 参考设计：顶部标题 + 新建；状态分段筛选；卡片含状态、日期、（可选）参与进度，
// 操作按钮仅在鼠标 hover 卡片时显现。项目量级小：一次性拉全量，前端过滤 + 分页。
import { computed, onMounted, ref, watch } from "vue";

import { Page, useVbenDrawer } from "@vben/common-ui";

import {
  Empty,
  message,
  Pagination,
  Popconfirm,
  Progress,
  RadioButton,
  RadioGroup,
  Spin,
  Tag,
} from "ant-design-vue";

import { $t } from "#/locales";
import {
  projectClose,
  projectDelete,
  projectDetail,
  projectList,
  projectReopen,
  projectSend,
  type ProjectListItem,
  type ProjectStatus,
} from "../eval-client";
import { notifySendResult } from "../send-result";
import ProjectDrawer from "./project-drawer.vue";

const emit = defineEmits<{
  openProject: [projectId: number];
}>();

const [ProjectDrawerRef, drawerApi] = useVbenDrawer({
  connectedComponent: ProjectDrawer,
});

const loading = ref(false);
const all = ref<ProjectListItem[]>([]);
// 每个项目的进度聚合（参与人数 + 完成/应完成），按需从 projectDetail 拉取并缓存。
// 列表接口不含进度，故对当前页可见项目单独取详情填充；缓存避免翻页/操作后重复请求。
const progressMap = ref<
  Record<
    number,
    { completed: number; evaluateeCount: number; expected: number }
  >
>({});
// 状态分段筛选：''=全部，其余为精确状态。名称搜索按参考图省略。
const queryStatus = ref<"" | ProjectStatus>("");

async function loadAll() {
  loading.value = true;
  try {
    all.value = (await projectList()) ?? [];
    // 数据刷新后清空进度缓存，交给 paged watch 重新按当前页拉取。
    progressMap.value = {};
  } finally {
    loading.value = false;
  }
}

onMounted(loadAll);

const filtered = computed(() => {
  const status = queryStatus.value;
  return status ? all.value.filter((p) => p.status === status) : all.value;
});

// 客户端分页：卡片过多时分页展示，不做无限滚动。每页 9 张。
const pageSize = ref(9);
const currentPage = ref(1);

const paged = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value;
  return filtered.value.slice(start, start + pageSize.value);
});

// 切换筛选时回到第一页，避免停留在越界的空页。
watch(queryStatus, () => {
  currentPage.value = 1;
});

// 为当前页可见项目拉取进度（参与人数取各问卷被评估人之和，完成率取项目总完成/应完成）。
async function loadProgress(ids: number[]) {
  await Promise.all(
    ids
      .filter((id) => !(id in progressMap.value))
      .map(async (id) => {
        try {
          const { detail } = await projectDetail(id);
          const evaluateeCount = (detail.questionnaires ?? []).reduce(
            (sum, q) => sum + (q.evaluateeCount ?? 0),
            0,
          );
          progressMap.value[id] = {
            evaluateeCount,
            completed: detail.totalCompleted,
            expected: detail.totalExpected,
          };
        } catch {
          // 单个项目进度加载失败不阻塞其它卡片。
        }
      }),
  );
}

// 当前页变化（翻页/筛选/刷新）即补齐可见项目的进度。
watch(
  paged,
  (list) => {
    loadProgress(list.map((p) => p.id));
  },
  { immediate: true },
);

// 状态分段选项：全部 + 三种状态。
const statusTabs: Array<{ label: string; value: "" | ProjectStatus }> = [
  { label: $t("plugin.linapro-eval-360.page.common.all"), value: "" },
  ...(["draft", "active", "closed"] as ProjectStatus[]).map((s) => ({
    label: $t(`plugin.linapro-eval-360.page.project.status.${s}`),
    value: s,
  })),
];

function statusColor(status: string) {
  if (status === "active") return "processing";
  if (status === "closed") return "default";
  return "warning";
}

function statusLabel(status: string) {
  return $t(`plugin.linapro-eval-360.page.project.status.${status}`);
}

// fmtDate 把 Unix 毫秒时间戳格式化为本地日期（YYYY-MM-DD，不展示时分秒）。
function fmtDate(ms: null | number) {
  if (!ms) return "";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "";
  const y = d.getFullYear();
  const m = `${d.getMonth() + 1}`.padStart(2, "0");
  const day = `${d.getDate()}`.padStart(2, "0");
  return `${y}-${m}-${day}`;
}

// 完成率：expected>0 时按 completed/expected 取整百分比。
function percentOf(x: { completed: number; expected: number }) {
  if (!x.expected || x.expected <= 0) return 0;
  return Math.round((x.completed / x.expected) * 100);
}

function handleCreate() {
  drawerApi.setData({ update: false });
  drawerApi.open();
}

function handleEdit(row: ProjectListItem) {
  drawerApi.setData({ id: row.id, update: true });
  drawerApi.open();
}

async function handleDelete(row: ProjectListItem) {
  await projectDelete(row.id);
  message.success($t("plugin.linapro-eval-360.page.project.deleteSuccess"));
  await loadAll();
}

async function handleClose(row: ProjectListItem) {
  await projectClose(row.id);
  message.success($t("plugin.linapro-eval-360.page.project.closeSuccess"));
  await loadAll();
}

async function handleReopen(row: ProjectListItem) {
  await projectReopen(row.id);
  message.success($t("plugin.linapro-eval-360.page.project.reopenSuccess"));
  await loadAll();
}

async function handleSend(row: ProjectListItem) {
  // 按问卷判定发布：推送失败的问卷保持未发布，由结果提示逐份列出；请求级失败由请求拦截器提示。
  notifySendResult(await projectSend(row.id));
  await loadAll();
}

function onReload() {
  return loadAll();
}
</script>

<template>
  <Page :auto-content-height="true">
    <div class="flex flex-col gap-4" data-testid="eval360-project-list">
      <!-- 顶部：标题 + 新建按钮。 -->
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-lg font-semibold">
          {{ $t("plugin.linapro-eval-360.page.project.title") }}
        </span>
        <a-button
          type="primary"
          class="ml-auto"
          data-testid="eval360-project-create"
          @click="handleCreate"
        >
          {{ $t("plugin.linapro-eval-360.page.project.create") }}
        </a-button>
      </div>

      <!-- 状态分段筛选。 -->
      <RadioGroup v-model:value="queryStatus" button-style="solid">
        <RadioButton v-for="t in statusTabs" :key="t.value" :value="t.value">
          {{ t.label }}
        </RadioButton>
      </RadioGroup>

      <Spin :spinning="loading">
        <Empty v-if="!filtered.length" />
        <div
          v-else
          class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3"
        >
          <div
            v-for="p in paged"
            :key="p.id"
            class="group flex cursor-pointer flex-col gap-2 rounded-xl border border-border bg-white p-5 shadow-sm transition-all hover:-translate-y-0.5 hover:border-primary hover:shadow-md dark:bg-slate-800"
            data-testid="eval360-project-card"
            @click="emit('openProject', p.id)"
          >
            <!-- 标题 + 状态标签。 -->
            <div class="flex items-start justify-between gap-2">
              <span class="text-base font-semibold leading-6">{{
                p.name
              }}</span>
              <Tag :color="statusColor(p.status)" class="shrink-0">
                {{ statusLabel(p.status) }}
              </Tag>
            </div>
            <!-- 日期区间。 -->
            <div class="mt-1 text-xs text-gray-500">
              {{ fmtDate(p.startAt) }} ~ {{ fmtDate(p.endAt) }}
            </div>
            <!-- 参与人数 + 完成进度（按需从项目详情拉取聚合，缓存于 progressMap）。 -->
            <div v-if="progressMap[p.id]" class="mt-3">
              <div
                class="flex items-center justify-between text-xs text-gray-500"
              >
                <span>
                  {{ $t("plugin.linapro-eval-360.page.project.participants") }}:
                  {{ progressMap[p.id]!.evaluateeCount }}
                </span>
                <span>{{ percentOf(progressMap[p.id]!) }}%</span>
              </div>
              <Progress
                :percent="percentOf(progressMap[p.id]!)"
                :show-info="false"
                size="small"
              />
            </div>
            <!-- 操作行：默认隐藏，hover 卡片时淡入（invisible 保留占位，避免高度跳动）。 -->
            <div
              class="invisible mt-3 flex flex-wrap gap-2 border-t pt-3 opacity-0 transition-all duration-200 group-hover:visible group-hover:opacity-100"
            >
              <a-button
                type="primary"
                size="small"
                data-testid="eval360-project-detail"
                @click.stop="emit('openProject', p.id)"
              >
                {{ $t("plugin.linapro-eval-360.page.project.detail") }}
              </a-button>
              <a-button size="small" @click.stop="handleEdit(p)">
                {{ $t("plugin.linapro-eval-360.page.common.edit") }}
              </a-button>
              <a-button
                v-if="p.status === 'active'"
                size="small"
                data-testid="eval360-project-send"
                @click.stop="handleSend(p)"
              >
                {{ $t("plugin.linapro-eval-360.page.project.send") }}
              </a-button>
              <Popconfirm
                v-if="!p.closedAt && p.status !== 'closed'"
                :title="$t('plugin.linapro-eval-360.page.project.closeConfirm')"
                @confirm="handleClose(p)"
              >
                <a-button size="small" @click.stop="">
                  {{ $t("plugin.linapro-eval-360.page.project.close") }}
                </a-button>
              </Popconfirm>
              <a-button
                v-else-if="p.closedAt"
                size="small"
                @click.stop="handleReopen(p)"
              >
                {{ $t("plugin.linapro-eval-360.page.project.reopen") }}
              </a-button>
              <Popconfirm
                :title="
                  $t('plugin.linapro-eval-360.page.project.deleteConfirm')
                "
                @confirm="handleDelete(p)"
              >
                <a-button danger size="small" @click.stop="">
                  {{ $t("plugin.linapro-eval-360.page.common.delete") }}
                </a-button>
              </Popconfirm>
            </div>
          </div>
        </div>

        <!-- 分页：仅当过滤结果超过一页时展示，右对齐。 -->
        <div v-if="filtered.length > pageSize" class="mt-4 flex justify-end">
          <Pagination
            v-model:current="currentPage"
            :page-size="pageSize"
            :total="filtered.length"
            :show-size-changer="false"
          />
        </div>
      </Spin>
    </div>

    <ProjectDrawerRef @reload="onReload" />
  </Page>
</template>
