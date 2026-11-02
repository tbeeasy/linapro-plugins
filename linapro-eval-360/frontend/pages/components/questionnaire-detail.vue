<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";

import { Input, message, Modal, Select } from "ant-design-vue";

import { $t } from "#/locales";
import {
  projectDetail,
  projectList,
  questionnaireClearAnswers,
  questionnaireCopy,
  questionnaireDetail,
  questionnairePublish,
  questionnaireUnpublish,
  relationList,
  type EvaluateeRow,
  type ProjectDetail,
  type ProjectListItem,
  type QuestionnaireDetail,
} from "../eval-client";
import QuestionnaireEditor from "./questionnaire-editor.vue";
import QuestionnairePreview from "./questionnaire-preview.vue";
import QuestionnairePreviewBody from "./questionnaire-preview-body.vue";
import RelationPanel from "./relation-panel.vue";
import ReportPanel from "./report-panel.vue";

const props = defineProps<{
  projectId: number;
  questionnaireId: number;
  // 进入时定位到的子菜单，由项目详情卡片的「评测问卷/评价关系」快捷入口传入。
  initialTab?: "questionnaire" | "relation" | "report";
}>();
// list=回项目列表（主页图标）；project=回当前项目的项目详情页；
// open=打开某项目下某问卷（项目/问卷联动下拉均走此口）。
// open 携带目标子菜单：复制问卷后需落在「评测问卷」继续编辑，其余场景默认评价关系。
const emit = defineEmits<{
  list: [];
  project: [projectId: number];
  open: [
    projectId: number,
    questionnaireId: number,
    tab?: "questionnaire" | "relation" | "report",
  ];
}>();

const detail = ref<QuestionnaireDetail | null>(null);
// 被评估人清单（来自评价关系）：矩阵题的「行」由此动态渲染，编辑器与预览共用。
// 切换问卷时随结构一并刷新，避免两处各拉一次。
const evaluatees = ref<EvaluateeRow[]>([]);
// 当前项目（含兄弟问卷列表）：问卷下拉数据来源。
const project = ref<null | ProjectDetail>(null);
// 全部项目：顶部「项目」下拉数据来源。
const projects = ref<ProjectListItem[]>([]);
const loading = ref(false);
const activeTab = ref<"questionnaire" | "relation" | "report">(
  props.initialTab ?? "relation",
);

// 编辑会话开关：评测问卷页默认不直接进编辑器，须先经「编辑问卷」三选一模式确认后
// 才置 true 渲染编辑器（问卷星式流程）。切换问卷时重置，避免跨问卷串编辑态。
const editStarted = ref(false);
// 是否为「保留答卷」受限模式：仅用于提示；破坏性编辑的强校验由后端白名单负责。
const editRestricted = ref(false);
// 编辑器实例：「完成编辑」前查询是否有未保存的题目卡片。
const editorRef = ref<InstanceType<typeof QuestionnaireEditor> | null>(null);
const publishing = ref(false);

// 问卷预览弹窗开关：与项目/问卷联动下拉同处顶栏，任意子菜单下均可查看只读结构。
const previewOpen = ref(false);

// ---- 「编辑问卷」三选一模式弹窗 ----
type EditMode = "clear" | "copy" | "keep";
const modeModalOpen = ref(false);
const selectedMode = ref<EditMode | null>(null);

// 复制问卷弹窗：新名称同项目内不可重名，故先弹窗让用户填名称。
const copyModalOpen = ref(false);
const copyTitle = ref("");

// 左侧竖排子菜单（问卷星式）：图标 + 文案，各自内容区渲染自身的操作。
const navItems = computed(() => [
  {
    key: "relation" as const,
    label: $t("plugin.linapro-eval-360.page.tab.relation"),
    icon: "icon-[ant-design--team-outlined]",
  },
  {
    key: "questionnaire" as const,
    label: $t("plugin.linapro-eval-360.page.tab.questionnaire"),
    icon: "icon-[ant-design--form-outlined]",
  },
  {
    key: "report" as const,
    label: $t("plugin.linapro-eval-360.page.tab.report"),
    icon: "icon-[ant-design--bar-chart-outlined]",
  },
]);

// 三选一编辑模式选项（对应问卷星「请选择修改问卷模式」卡片）。
const editModeOptions = computed(() => [
  {
    key: "keep" as const,
    title: $t("plugin.linapro-eval-360.page.questionnaire.editMode.keepTitle"),
    desc: $t("plugin.linapro-eval-360.page.questionnaire.editMode.keepDesc"),
    icon: "icon-[ant-design--pie-chart-outlined]",
  },
  {
    key: "clear" as const,
    title: $t("plugin.linapro-eval-360.page.questionnaire.editMode.clearTitle"),
    desc: $t("plugin.linapro-eval-360.page.questionnaire.editMode.clearDesc"),
    icon: "icon-[ant-design--delete-outlined]",
  },
  {
    key: "copy" as const,
    title: $t("plugin.linapro-eval-360.page.questionnaire.editMode.copyTitle"),
    desc: $t("plugin.linapro-eval-360.page.questionnaire.editMode.copyDesc"),
    icon: "icon-[ant-design--copy-outlined]",
  },
]);

const published = computed(() => detail.value?.published === 1);

// 编辑是否受限：已发布 或 已有作答。二者任一成立即须走三选一策略；
// 未发布且无作答的草稿可直接自由编辑。与后端 editRestricted 闸门语义一致。
const restricted = computed(
  () => published.value || detail.value?.hasAnswers === true,
);

// 问卷是否已有题目：有题目时评测问卷页未编辑态直接内嵌整份预览，无题目时才显示编辑引导。
const hasQuestions = computed(() =>
  (detail.value?.dimensions ?? []).some((d) => d.questions.length > 0),
);

// 切换到不同问卷时重置子菜单与编辑态，并重新拉取问卷结构（组件保持挂载，需手动刷新）。
watch(
  () => props.questionnaireId,
  () => {
    activeTab.value = props.initialTab ?? "relation";
    resetEditState();
    refresh();
  },
);

function resetEditState() {
  editStarted.value = false;
  editRestricted.value = false;
  modeModalOpen.value = false;
  selectedMode.value = null;
  copyModalOpen.value = false;
}

async function refresh() {
  loading.value = true;
  try {
    const res = await questionnaireDetail(props.questionnaireId);
    detail.value = res.detail;
    // 同步加载被评估人清单（矩阵题行来源），失败不阻断问卷结构展示。
    try {
      const matrix = await relationList(props.questionnaireId);
      // 后端空切片会序列化为 null，这里兜底为空数组，避免下游非空数组 prop 校验失败。
      evaluatees.value = matrix.evaluatees ?? [];
    } catch {
      evaluatees.value = [];
    }
    // 用问卷自身的 projectId 兜底加载项目（问卷下拉选项来源），
    // 避免 props.projectId 缺失时切换器拿不到选项而消失。
    await loadProject(detail.value?.projectId ?? props.projectId);
  } finally {
    loading.value = false;
  }
}

// 加载当前项目：复用 projectDetail 拿到项目名与兄弟问卷；同项目不重复拉取。
async function loadProject(pid?: number) {
  if (!pid || project.value?.id === pid) return;
  const res = await projectDetail(pid);
  project.value = res.detail;
}

onMounted(() => {
  refresh();
  loadProjects();
});

// 加载全部项目：顶部「项目」下拉选项。
async function loadProjects() {
  projects.value = await projectList();
}

// 当前项目 id：优先已加载项目，兜底用 props。
const currentProjectId = computed(() => project.value?.id ?? props.projectId);

// 顶部「项目」下拉选项。
const projectOptions = computed(() =>
  projects.value.map((p) => ({ label: p.name, value: p.id })),
);

// 当前项目的问卷切换选项（与「项目」下拉联动：项目变→问卷选项随之变）。
const switchOptions = computed(() =>
  (project.value?.questionnaires ?? []).map((q) => ({
    label: q.title,
    value: q.id,
  })),
);

// 项目下拉切换：加载目标项目并跳到其第一份问卷；无问卷则提示并保持当前选中。
async function onProjectChange(pid: number) {
  if (!pid || pid === currentProjectId.value) return;
  const res = await projectDetail(pid);
  const first = res.detail.questionnaires[0];
  if (!first) {
    message.info($t("plugin.linapro-eval-360.page.questionnaire.emptyProject"));
    return;
  }
  emit("open", pid, first.id);
}

// 问卷下拉切换：非当前问卷才在同项目内切换。
function onSwitch(value: number) {
  if (value && value !== props.questionnaireId) {
    emit("open", currentProjectId.value, value);
  }
}

// 编辑器/关系面板保存后刷新结构树。
function onStructureChanged() {
  refresh();
}

// ---- 「编辑问卷」入口与三选一模式 ----

// 打开模式弹窗：无论当前在哪个子菜单，都先切到评测问卷。
// 草稿（未发布且无作答）编辑不受限，直接进编辑器、跳过三选一；否则弹模式选择。
function openModeModal() {
  activeTab.value = "questionnaire";
  if (!restricted.value) {
    editRestricted.value = false;
    editStarted.value = true;
    return;
  }
  selectedMode.value = null;
  modeModalOpen.value = true;
}

// 「完成编辑」：退出编辑会话回到内嵌预览。维度增删改即时落库，
// 仅题目卡片有本地草稿，存在未保存卡片时先二次确认，避免静默丢弃。
function finishEdit() {
  const exit = () => {
    editStarted.value = false;
    editRestricted.value = false;
  };
  if (!editorRef.value?.hasUnsaved()) {
    exit();
    return;
  }
  Modal.confirm({
    title: $t("plugin.linapro-eval-360.page.questionnaire.finishEditUnsavedTitle"),
    content: $t(
      "plugin.linapro-eval-360.page.questionnaire.finishEditUnsavedContent",
    ),
    okType: "danger",
    onOk: exit,
  });
}

// 「下一步」：按选中模式进入编辑。
// keep=直接进编辑器（受限提示，破坏性编辑由后端白名单拦）；
// clear=二次确认→清空作答→进不受限编辑器；
// copy=弹窗填新名→复制→跳转到新问卷的评测问卷继续编辑。
async function confirmMode() {
  const mode = selectedMode.value;
  if (!mode) return;
  if (mode === "keep") {
    editRestricted.value = true;
    editStarted.value = true;
    modeModalOpen.value = false;
    return;
  }
  if (mode === "clear") {
    modeModalOpen.value = false;
    Modal.confirm({
      title: $t(
        "plugin.linapro-eval-360.page.questionnaire.clearConfirmTitle",
      ),
      content: $t(
        "plugin.linapro-eval-360.page.questionnaire.clearConfirmContent",
      ),
      okType: "danger",
      onOk: async () => {
        await questionnaireClearAnswers(props.questionnaireId);
        message.success(
          $t("plugin.linapro-eval-360.page.common.operateSuccess"),
        );
        editRestricted.value = false;
        editStarted.value = true;
        await refresh();
      },
    });
    return;
  }
  // copy：先关模式弹窗，再开复制命名弹窗。
  copyTitle.value = suggestCopyTitle(detail.value?.title ?? "");
  modeModalOpen.value = false;
  copyModalOpen.value = true;
}

// suggestCopyTitle 生成一个当前未被占用的建议名（仅本地判断，最终以后端查重为准）。
function suggestCopyTitle(sourceTitle: string): string {
  const taken = new Set(
    (project.value?.questionnaires ?? []).map((q) => q.title.trim()),
  );
  for (let n = 1; n < 100; n += 1) {
    const suffix = n === 1 ? "(副本)" : `(副本 ${n})`;
    const candidate = `${sourceTitle} ${suffix}`;
    if (!taken.has(candidate)) return candidate;
  }
  return `${sourceTitle} (副本)`;
}

async function confirmCopy() {
  const title = copyTitle.value.trim();
  if (!title) {
    message.warning(
      $t("plugin.linapro-eval-360.page.questionnaire.copyNameRequired"),
    );
    return;
  }
  try {
    const res = await questionnaireCopy(props.questionnaireId, title);
    message.success(
      $t("plugin.linapro-eval-360.page.questionnaire.copySuccess"),
    );
    copyModalOpen.value = false;
    // 跳转到复制出的新问卷并落在评测问卷子菜单（「复制并去编辑」）。
    emit("open", currentProjectId.value, res.id, "questionnaire");
  } catch {
    // 重名/非法名称的中文提示已由请求拦截器统一处理，弹窗保持打开供用户改名。
  }
}

// ---- 「发放问卷」问卷级发布/取消发布 ----

function handleDistribute() {
  if (!published.value) {
    // 发布会立即向评价者推送卡片，先二次确认。
    Modal.confirm({
      title: $t(
        "plugin.linapro-eval-360.page.questionnaire.publishConfirmTitle",
      ),
      content: $t(
        "plugin.linapro-eval-360.page.questionnaire.publishConfirmContent",
      ),
      onOk: () => doPublish(),
    });
    return;
  }
  // 取消发布会让评价者无法继续作答，先二次确认。
  Modal.confirm({
    title: $t(
      "plugin.linapro-eval-360.page.questionnaire.unpublishConfirmTitle",
    ),
    content: $t(
      "plugin.linapro-eval-360.page.questionnaire.unpublishConfirmContent",
    ),
    okType: "danger",
    onOk: () => doUnpublish(),
  });
}

async function doUnpublish() {
  publishing.value = true;
  try {
    await questionnaireUnpublish(props.questionnaireId);
    message.success(
      $t("plugin.linapro-eval-360.page.questionnaire.unpublishSuccess"),
    );
    await refresh();
  } catch {
    // 失败的中文提示由请求拦截器统一处理。
  } finally {
    publishing.value = false;
  }
}

async function doPublish() {
  publishing.value = true;
  try {
    // 发布即向评价者推送卡片，全部评价者送达才发布（同一键发送）。
    await questionnairePublish(props.questionnaireId);
    message.success(
      $t("plugin.linapro-eval-360.page.questionnaire.publishSuccess"),
    );
    await refresh();
  } catch {
    // 发布前权重校验与推送失败（含失败名单）的提示由请求拦截器统一处理，问卷保持未发布。
  } finally {
    publishing.value = false;
  }
}
</script>

<template>
  <!-- 整体左右布局：左=全高导航栏，右=顶部固定下拉 + 下方滚动内容。 -->
  <div class="flex h-full gap-3" data-testid="eval360-questionnaire-detail">
    <!-- 左侧竖排子菜单（问卷星式，顶部主页图标回项目列表），占满整列高度。 -->
    <nav
      class="flex w-28 shrink-0 flex-col gap-1 rounded-xl bg-slate-800 p-2"
      data-testid="eval360-questionnaire-nav"
    >
      <!-- 主页：直接返回项目列表。 -->
      <button
        type="button"
        class="mb-1 flex flex-col items-center gap-1.5 rounded-lg border-b border-slate-700 px-2 pb-3 pt-2 text-sm text-slate-300 transition-colors hover:bg-slate-700 hover:text-white"
        data-testid="eval360-questionnaire-home"
        @click="emit('list')"
      >
        <span class="icon-[ant-design--home-outlined] text-xl"></span>
        <span>{{ $t("plugin.linapro-eval-360.page.common.projectList") }}</span>
      </button>
      <button
        v-for="item in navItems"
        :key="item.key"
        type="button"
        class="flex flex-col items-center gap-1.5 rounded-lg px-2 py-3 text-sm transition-colors"
        :class="
          activeTab === item.key
            ? 'bg-primary text-white'
            : 'text-slate-300 hover:bg-slate-700 hover:text-white'
        "
        :data-testid="`eval360-nav-${item.key}`"
        @click="activeTab = item.key"
      >
        <span :class="item.icon" class="text-xl"></span>
        <span>{{ item.label }}</span>
      </button>
    </nav>

    <!-- 右侧：顶部固定「项目 + 问卷」联动下拉，下方内容区独立滚动。 -->
    <div class="flex min-h-0 flex-1 flex-col gap-3">
      <!-- 顶部固定下拉条：选中项目→联动其问卷列表；右侧「编辑问卷 / 发放问卷」入口。 -->
      <div
        class="flex flex-wrap items-center gap-2 rounded-xl border border-border bg-white p-3 shadow-sm dark:bg-slate-800"
      >
        <Select
          :value="currentProjectId"
          :options="projectOptions"
          class="min-w-[220px]"
          show-search
          option-filter-prop="label"
          :placeholder="$t('plugin.linapro-eval-360.page.common.projectList')"
          @change="onProjectChange"
        />
        <span
          class="icon-[ant-design--right-outlined] text-xs text-gray-300"
        ></span>
        <Select
          :value="questionnaireId"
          :options="switchOptions"
          class="min-w-[240px]"
          show-search
          option-filter-prop="label"
          @change="onSwitch"
        />
        <!-- 问卷下拉右侧：项目详情入口（回到当前项目的项目详情页）。 -->
        <a-button
          data-testid="eval360-questionnaire-project-entry"
          @click="emit('project', currentProjectId)"
        >
          {{ $t("plugin.linapro-eval-360.page.common.projectDetail") }}
        </a-button>
        <!-- 预览按钮：仅在评测问卷子菜单的编辑态显示；未编辑时内容区已内嵌整份预览，无需重复入口。 -->
        <a-button
          v-if="activeTab === 'questionnaire' && editStarted"
          data-testid="eval360-questionnaire-preview-entry"
          @click="previewOpen = true"
        >
          {{ $t("plugin.linapro-eval-360.page.questionnaire.previewEntry") }}
        </a-button>
        <!-- 问卷下拉之后的入口按钮：仅在评测问卷子菜单显示（编辑问卷 + 发放问卷）。 -->
        <div
          v-if="activeTab === 'questionnaire'"
          class="ml-auto flex items-center gap-2"
        >
          <a-button
            data-testid="eval360-questionnaire-edit-entry"
            @click="openModeModal"
          >
            {{ $t("plugin.linapro-eval-360.page.questionnaire.editEntry") }}
          </a-button>
          <a-button
            type="primary"
            :loading="publishing"
            data-testid="eval360-questionnaire-publish"
            @click="handleDistribute"
          >
            {{
              published
                ? $t("plugin.linapro-eval-360.page.questionnaire.unpublish")
                : $t("plugin.linapro-eval-360.page.questionnaire.distribute")
            }}
          </a-button>
        </div>
      </div>

      <!-- 内容区：各子菜单渲染各自的操作（编辑器工具 / 关系导入导出 / 报表导出）。 -->
      <div
        class="min-h-0 flex-1 overflow-y-auto rounded-xl border border-border bg-white p-5 shadow-sm dark:bg-slate-800"
      >
        <!-- 评测问卷：默认不直接进编辑器，先经「编辑问卷」选模式；选定后才渲染编辑器。 -->
        <template v-if="activeTab === 'questionnaire'">
          <div
            v-if="editStarted && detail"
            class="flex h-full flex-col gap-3"
            data-testid="eval360-questionnaire-edit-session"
          >
            <div
              class="flex items-center justify-between gap-2 rounded-lg px-3 py-2 text-sm"
              :class="
                editRestricted
                  ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
                  : 'bg-gray-50 text-gray-500 dark:bg-slate-700/50 dark:text-gray-300'
              "
            >
              <span v-if="editRestricted">
                {{
                  $t(
                    "plugin.linapro-eval-360.page.questionnaire.restrictedBanner",
                  )
                }}
              </span>
              <span v-else>
                {{
                  $t("plugin.linapro-eval-360.page.questionnaire.editMode.clearDesc")
                }}
              </span>
              <span class="flex shrink-0 items-center">
                <a-button
                  v-if="restricted"
                  type="link"
                  size="small"
                  @click="openModeModal"
                >
                  {{
                    $t("plugin.linapro-eval-360.page.questionnaire.editReselect")
                  }}
                </a-button>
                <a-button
                  type="primary"
                  size="small"
                  data-testid="eval360-questionnaire-finish-edit"
                  @click="finishEdit"
                >
                  {{
                    $t("plugin.linapro-eval-360.page.questionnaire.finishEdit")
                  }}
                </a-button>
              </span>
            </div>
            <div class="min-h-0 flex-1">
              <QuestionnaireEditor
                ref="editorRef"
                :questionnaire="detail"
                :evaluatees="evaluatees"
                @changed="onStructureChanged"
              />
            </div>
          </div>
          <!-- 未进入编辑且已有题目：直接内嵌整份问卷的只读预览。 -->
          <QuestionnairePreviewBody
            v-else-if="detail && hasQuestions"
            :detail="detail"
            :evaluatees="evaluatees"
            data-testid="eval360-questionnaire-inline-preview"
          />
          <!-- 未进入编辑且暂无题目：引导先点「编辑问卷」选择模式。 -->
          <div
            v-else
            class="flex h-full flex-col items-center justify-center gap-4 text-center"
            data-testid="eval360-questionnaire-edit-landing"
          >
            <span
              class="icon-[ant-design--form-outlined] text-5xl text-gray-300"
            ></span>
            <p class="max-w-md text-sm text-gray-500">
              {{
                $t("plugin.linapro-eval-360.page.questionnaire.editLandingHint")
              }}
            </p>
            <a-button type="primary" @click="openModeModal">
              {{ $t("plugin.linapro-eval-360.page.questionnaire.editEntry") }}
            </a-button>
          </div>
        </template>
        <RelationPanel
          v-else-if="activeTab === 'relation' && detail"
          :questionnaire-id="detail.id"
          :published="detail.published"
          :roles="detail.roles"
          @changed="onStructureChanged"
        />
        <ReportPanel
          v-else-if="activeTab === 'report'"
          :questionnaire-id="props.questionnaireId"
        />
      </div>
    </div>

    <!-- 问卷预览弹窗：只读渲染当前问卷结构（被评估人视角）。 -->
    <QuestionnairePreview
      v-model:open="previewOpen"
      :detail="detail"
      :evaluatees="evaluatees"
    />

    <!-- 「编辑问卷」三选一模式弹窗（问卷星「请选择修改问卷模式」）。 -->
    <Modal
      :open="modeModalOpen"
      :title="$t('plugin.linapro-eval-360.page.questionnaire.editMode.title')"
      :width="720"
      :footer="null"
      data-testid="eval360-edit-mode-modal"
      @cancel="modeModalOpen = false"
    >
      <div class="flex flex-col gap-4 py-2">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <button
            v-for="opt in editModeOptions"
            :key="opt.key"
            type="button"
            class="flex flex-col items-center gap-2 rounded-lg border-2 px-3 py-5 text-center transition-colors"
            :class="
              selectedMode === opt.key
                ? 'border-primary bg-primary/5'
                : 'border-border hover:border-primary/60'
            "
            :data-testid="`eval360-edit-mode-${opt.key}`"
            @click="selectedMode = opt.key"
          >
            <span
              :class="opt.icon"
              class="text-3xl"
              :style="selectedMode === opt.key ? '' : 'opacity:0.5'"
            ></span>
            <span class="font-semibold">{{ opt.title }}</span>
            <span class="text-xs text-gray-500">{{ opt.desc }}</span>
          </button>
        </div>
        <div
          class="flex flex-col gap-1 rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-slate-700/50 dark:text-gray-300"
        >
          <span>
            {{
              $t("plugin.linapro-eval-360.page.questionnaire.editMode.hintAllow")
            }}
          </span>
          <span>
            {{
              $t(
                "plugin.linapro-eval-360.page.questionnaire.editMode.hintForbid",
              )
            }}
          </span>
        </div>
        <div class="flex justify-center">
          <a-button
            type="primary"
            :disabled="!selectedMode"
            data-testid="eval360-edit-mode-next"
            @click="confirmMode"
          >
            {{ $t("plugin.linapro-eval-360.page.questionnaire.editMode.next") }}
          </a-button>
        </div>
      </div>
    </Modal>

    <!-- 复制问卷弹窗：新名称同项目内不可重名，故不沿用源标题。 -->
    <Modal
      :open="copyModalOpen"
      :title="$t('plugin.linapro-eval-360.page.questionnaire.copyTitle')"
      :ok-text="$t('plugin.linapro-eval-360.page.common.confirm')"
      :cancel-text="$t('plugin.linapro-eval-360.page.common.cancel')"
      @ok="confirmCopy"
      @cancel="copyModalOpen = false"
    >
      <div class="flex flex-col gap-2">
        <span class="text-sm text-gray-500">
          {{
            $t("plugin.linapro-eval-360.page.questionnaire.copyHint", {
              source: detail?.title ?? "",
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
