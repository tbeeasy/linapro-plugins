<script setup lang="ts">
import { computed, ref, watch } from "vue";

import {
  Divider,
  Form,
  FormItem,
  message,
  Select,
  Switch,
  Tag,
} from "ant-design-vue";

import { $t } from "#/locales";
import { TiptapEditor } from "#/components/tiptap";
import {
  type EvaluateeRow,
  type QuestionnaireDetail,
  type QuestionOptionDTO,
  type QuestionSaveInput,
} from "../eval-client";
import MatrixTable from "./matrix-table.vue";
import MatrixColumnsModal from "./matrix-columns-modal.vue";
import MatrixColumnCommentModal from "./matrix-column-comment-modal.vue";
import SingleChoiceOptionEditor from "./single-choice-option-editor.vue";

const props = defineProps<{
  questionnaire: QuestionnaireDetail;
  dimensionId: number;
  // 被评估人清单（来自评价关系树）：矩阵题的「行」由此动态渲染，不再手填；
  // 受限评价者选项也取自其 groups[].items[] 的评价者（按 EmpID 去重）。
  evaluatees: EvaluateeRow[];
  // 保存回调：新建/编辑题目。返回 Promise，便于子组件在真正落库成功后再收起本地草稿卡
  // （失败时保留草稿，由响应拦截器统一提示）。
  saveHandler?: (
    input: QuestionSaveInput,
    id?: number,
    insertIndex?: number,
  ) => Promise<void>;
}>();
const emit = defineEmits<{
  delete: [id: number];
}>();

const dimension = computed(() =>
  props.questionnaire.dimensions.find((d) => d.id === props.dimensionId),
);

// 当前维度的题号起始偏移：编辑器一次只渲染一个维度的题目，
// 但题号需在整份问卷上连续（不能每个维度都从 1 起），故累计排在它前面的各维度题目数。
const dimensionBaseOffset = computed(() => {
  let offset = 0;
  for (const d of props.questionnaire.dimensions ?? []) {
    if (d.id === props.dimensionId) break;
    offset += d.questions.length;
  }
  return offset;
});

// 题型选项。
const typeOptions = computed(() => [
  {
    label: $t("plugin.linapro-eval-360.page.questionnaire.type.singleChoice"),
    value: "single_choice",
  },
  {
    label: $t("plugin.linapro-eval-360.page.questionnaire.type.textShort"),
    value: "text_short",
  },
  {
    label: $t("plugin.linapro-eval-360.page.questionnaire.type.textLong"),
    value: "text_long",
  },
  {
    label: $t("plugin.linapro-eval-360.page.questionnaire.type.matrix"),
    value: "matrix",
  },
]);

// 当前维度下的题目（编辑器每张卡片对应一道题）。
type QuestionNode =
  QuestionnaireDetail["dimensions"][number]["questions"][number];

interface DraftOption extends QuestionOptionDTO {}
// 矩阵列草稿：文案 + 分值 + 排序 + 评语配置（开关/必填/提示语/预设/允许自定义）。
interface DraftColumn {
  id: number;
  label: string;
  score: string;
  sort: number;
  allowComment: number;
  commentRequired: number;
  commentPlaceholder: string;
  commentPresets: string[];
  commentAllowCustom: number;
}

interface DraftQuestion {
  type: "single_choice" | "text_short" | "text_long" | "matrix";
  title: string;
  description: string;
  required: number;
  options: DraftOption[];
  // matrixColumns = 矩阵题的评价项（列）：文案 + 分值，经弹窗编辑。
  matrixColumns: DraftColumn[];
  visibleRoleIds: number[];
  excludedEvaluatorEmpIds: number[];
}

// 一张卡片 = 一道题。id>0 为已存题（默认只读，hover 出现「编辑」「在此后插入」）；
// id=0 为新增草稿卡（恒处于编辑态），由底部「新增题目」按钮或「在此后插入」生成。
interface CardState {
  // 卡片在当前渲染中的稳定键（已存卡为题目 id，草稿卡为负值）。
  key: number;
  // 题目 id；0 表示尚未落库的新增卡。
  id: number;
  draft: DraftQuestion;
  // 最近一次与后端一致的状态快照（JSON），用于判断卡片是否有未保存改动。
  saved: string;
  // 是否填写描述：开关开启才显示描述输入框；关闭时保存为空描述。
  showDescription: boolean;
  saving: boolean;
  // 是否处于编辑态：已存卡片默认只读（false），点「编辑」后为 true；草稿卡恒为 true。
  editing: boolean;
  // 草稿卡的插入锚点：0=追加到底部；>0=插在该已存题目之后（已存卡恒为 0）。
  insertAfter: number;
}

function cloneDraft(q: QuestionNode): DraftQuestion {
  return {
    type: q.type,
    title: q.title,
    description: q.description ?? "",
    required: q.required,
    options: q.options.map((o) => ({ ...o })),
    matrixColumns: q.matrixColumns.map((c) => ({ ...c })),
    visibleRoleIds: [...q.visibleRoleIds],
    excludedEvaluatorEmpIds: [...q.excludedEvaluatorEmpIds],
  };
}

// blankColumn 生成一列空白草稿（id=0 表示新增）：默认不配置评语。
function blankColumn(label: string, score: string, sort: number): DraftColumn {
  return {
    id: 0,
    label,
    score,
    sort,
    allowComment: 0,
    commentRequired: 0,
    commentPlaceholder: "",
    commentPresets: [],
    commentAllowCustom: 0,
  };
}

function blankDraft(): DraftQuestion {
  return {
    type: "single_choice",
    title: "",
    description: "",
    required: 0,
    options: [{ id: 0, label: "", score: "0", allowText: 0, sort: 0 }],
    matrixColumns: [
      {
        ...blankColumn(
          $t("plugin.linapro-eval-360.page.questionnaire.matrixCol1"),
          "1",
          0,
        ),
      },
      {
        ...blankColumn(
          $t("plugin.linapro-eval-360.page.questionnaire.matrixCol2"),
          "2",
          1,
        ),
      },
    ],
    visibleRoleIds: [],
    excludedEvaluatorEmpIds: [],
  };
}

// htmlIsEmpty 判断富文本内容是否为空（去标签 + 实体空格 + 空白后为空）。
// TiptapEditor 空内容会产出 `<p></p>`，不能直接 trim 判空。
function htmlIsEmpty(html: string): boolean {
  if (!html) return true;
  const text = html
    .replace(/<[^>]*>/g, "")
    .replace(/&nbsp;/gi, " ")
    .trim();
  return text.length === 0;
}

// snapshot 生成卡片草稿的 JSON 快照，用于脏检查（与 saved 比对）。
function snapshot(d: DraftQuestion): string {
  return JSON.stringify(d);
}

// 题库卡片列表：每道已存题目一张 + 若干新增草稿卡（由底部「新增题目」或「在此后插入」生成）。
const cards = ref<CardState[]>([]);

// 草稿卡的自增负值键（已存卡用正数题目 id 作键，两者不会冲突）。
let draftKeySeq = 0;
function nextDraftKey(): number {
  draftKeySeq -= 1;
  return draftKeySeq;
}

function makeDraftCard(insertAfter: number): CardState {
  const draft = blankDraft();
  return {
    key: nextDraftKey(),
    id: 0,
    draft,
    saved: snapshot(draft),
    showDescription: false,
    saving: false,
    editing: true,
    insertAfter,
  };
}

// 用后端题目列表重建卡片，同时保留本地未保存的编辑与草稿卡：
//   - 同 id 且存在未保存改动的已存卡：沿用本地 draft（避免父组件 refetch 后把输入吞掉）；
//   - 草稿卡：按 insertAfter 锚点重新插回对应位置（锚点为 0 或锚点题被删则退到底部）。
function syncCards() {
  const questions = dimension.value?.questions ?? [];
  const prevSaved = new Map<number, CardState>();
  const prevDrafts: CardState[] = [];
  for (const c of cards.value) {
    if (c.id > 0) prevSaved.set(c.id, c);
    else prevDrafts.push(c);
  }

  const next: CardState[] = [];
  const placedDrafts = new Set<CardState>();
  for (const q of questions) {
    const prev = prevSaved.get(q.id);
    if (prev && snapshot(prev.draft) !== prev.saved) {
      // 有未保存改动：保留本地编辑态（含编辑中状态）。
      next.push(prev);
    } else {
      const draft = cloneDraft(q);
      next.push({
        key: q.id,
        id: q.id,
        draft,
        saved: snapshot(draft),
        showDescription: !htmlIsEmpty(q.description ?? ""),
        saving: false,
        // 已存卡片默认只读，hover 点「编辑」才进入编辑态。
        editing: false,
        insertAfter: 0,
      });
    }
    // 把锚定在这道题之后的草稿卡插进来（保持原有先后顺序）。
    for (const d of prevDrafts) {
      if (d.insertAfter === q.id && !placedDrafts.has(d)) {
        placedDrafts.add(d);
        next.push(d);
      }
    }
  }
  // 锚点为 0（追加到底部）或锚点题已被删除的草稿卡统一退到底部，保证用户正在录入的新题不丢。
  for (const d of prevDrafts) {
    if (!placedDrafts.has(d)) next.push(d);
  }
  cards.value = next;
}

watch(
  () => dimension.value?.questions,
  () => syncCards(),
  { immediate: true, deep: true },
);

// 评价项（矩阵列）弹窗编辑：列编辑逻辑与转换都在 MatrixColumnsModal 内，
// 此处只负责「哪张卡片正在编辑」并把结果写回该卡片草稿。
const columnModalOpen = ref(false);
const columnTargetCard = ref<CardState | null>(null);

function openColumnModal(card: CardState) {
  columnTargetCard.value = card;
  columnModalOpen.value = true;
}

function submitColumnModal(cols: DraftColumn[]) {
  if (columnTargetCard.value) {
    columnTargetCard.value.draft.matrixColumns = cols.map((c, i) => ({
      ...c,
      sort: i,
    }));
  }
  columnModalOpen.value = false;
}

// ---- 列头操作栏（⋮）：删除本列 / 允许评语 / 左移 / 右移 ----
// 四个操作都只改当前卡片的草稿，不单独调接口：随「保存题目」一并落库。
// 因此存在未保存标记（isDirty），用户可先取消编辑放弃整批改动。

// 删除本列：直接从草稿移除并重排 sort；最后一列不允许删除（题目至少需要一个评价项）。
function deleteMatrixColumn(card: CardState, index: number) {
  if (card.draft.matrixColumns.length <= 1) {
    message.warning(
      $t("plugin.linapro-eval-360.page.questionnaire.matrixRequired"),
    );
    return;
  }
  card.draft.matrixColumns.splice(index, 1);
  renumberColumns(card);
}

// moveMatrixColumn 交换相邻两列（dir=-1 左移，dir=1 右移），首末列不动。
function moveMatrixColumn(card: CardState, index: number, dir: -1 | 1) {
  const target = index + dir;
  const cols = card.draft.matrixColumns;
  if (target < 0 || target >= cols.length) return;
  [cols[index], cols[target]] = [cols[target]!, cols[index]!];
  renumberColumns(card);
}

// renumberColumns 按当前顺序重写 sort，保证排序与视觉顺序一致。
function renumberColumns(card: CardState) {
  card.draft.matrixColumns.forEach((c, i) => {
    c.sort = i;
  });
}

// toggleAllowComment 切换「允许评语」总开关；关闭时同时关闭必填，
// 避免留下「不允许评语却要求必填」的不可达配置。
function toggleAllowComment(card: CardState, index: number) {
  const col = card.draft.matrixColumns[index];
  if (!col) return;
  const next = col.allowComment === 1 ? 0 : 1;
  col.allowComment = next;
  if (next === 0) col.commentRequired = 0;
}

// toggleCommentRequired 切换「必填」子项（仅允许评语开启时出现）。
function toggleCommentRequired(card: CardState, index: number) {
  const col = card.draft.matrixColumns[index];
  if (!col) return;
  col.commentRequired = col.commentRequired === 1 ? 0 : 1;
}

// 评语配置弹窗：同一弹窗按 mode 复用于「提示语」与「预设填空」。
const commentModalOpen = ref(false);
const commentModalMode = ref<"placeholder" | "presets">("placeholder");
const commentTargetCard = ref<CardState | null>(null);
const commentTargetIndex = ref(-1);

function openCommentModal(
  card: CardState,
  index: number,
  mode: "placeholder" | "presets",
) {
  commentTargetCard.value = card;
  commentTargetIndex.value = index;
  commentModalMode.value = mode;
  commentModalOpen.value = true;
}

// 弹窗目标列的当前值：用于给弹窗回显（关闭后目标消失时返回空值兜底）。
const commentTargetColumn = computed<DraftColumn | undefined>(() => {
  const card = commentTargetCard.value;
  const index = commentTargetIndex.value;
  if (!card || index < 0) return undefined;
  return card.draft.matrixColumns[index];
});

function submitCommentModal(payload: {
  placeholder: string;
  presets: string[];
  allowCustom: number;
}) {
  const col = commentTargetColumn.value;
  if (col) {
    if (commentModalMode.value === "placeholder") {
      col.commentPlaceholder = payload.placeholder;
    } else {
      col.commentPresets = payload.presets;
      col.commentAllowCustom = payload.allowCustom;
    }
  }
  commentModalOpen.value = false;
}

// 矩阵预览行：一行一个被评估人（来自评价关系）。无关系时为空数组，
// 由 MatrixTable 渲染灰色占位行，保证未导入关系也能看到并配置列。
const matrixRows = computed(() => props.evaluatees.map((e) => e.evaluateeName));

// 矩阵行提示：有关系时的「行=被评估人」是既成事实；无关系时改为引导先配列、
// 导入关系后自动成行，避免误以为必须先导入关系才能配置这道题。
const matrixRowsHintKey = computed(() =>
  matrixRows.value.length > 0 ? "matrixRowsHint" : "matrixRowsHintEmpty",
);

// 二维可见性的两个选项源（均取自 questionnaire / 评价关系，同源 relation-panel）：
//   - 可见角色：取自问卷角色集合（导入关系时动态生成）。
//   - 受限评价者：取自关系树 evaluatees[].groups[].items[] 按评价者 EmpID 去重。
//     名单按评价者本人全局生效（与其评谁无关），value 即后端排除名单比对的 evaluatorEmpId；
//     label 附工号以区分同名评价者。
const roleOptions = computed(() =>
  (props.questionnaire.roles ?? []).map((r) => ({
    label: r.name,
    value: r.id,
  })),
);

// evaluatorOptionsFor 为指定卡片生成受限评价者选项列表：
//   - 未选任何可见角色：可见角色为空 = 全员可见，故全部评价者都可被限制；
//   - 已选可见角色：仅包含这些角色下的评价者（其他角色本就看不到该题，无需再限）。
function evaluatorOptionsFor(
  card: CardState,
): { label: string; value: number }[] {
  const visibleRoleSet = new Set(card.draft.visibleRoleIds);
  const allRoles = visibleRoleSet.size === 0;
  const seen = new Map<number, { label: string; value: number }>();
  for (const e of props.evaluatees) {
    for (const g of e.groups ?? []) {
      // 选了可见角色时，仅收集勾选角色下的评价者。
      if (!allRoles && !visibleRoleSet.has(g.roleId)) continue;
      for (const it of g.items ?? []) {
        if (seen.has(it.evaluatorEmpId)) continue;
        const label = it.evaluatorNo
          ? `${it.evaluatorName}（${it.evaluatorNo}）`
          : it.evaluatorName;
        seen.set(it.evaluatorEmpId, { label, value: it.evaluatorEmpId });
      }
    }
  }
  return [...seen.values()];
}

// onVisibleRolesChange 当可见角色变化时，清理不再合法的受限评价者（即不在新选项集中的 EmpID）。
// 清空可见角色时选项集为全部评价者，已选名单不会被清掉。
function onVisibleRolesChange(card: CardState) {
  const allowedSet = new Set(evaluatorOptionsFor(card).map((opt) => opt.value));
  card.draft.excludedEvaluatorEmpIds =
    card.draft.excludedEvaluatorEmpIds.filter((id) => allowedSet.has(id));
}

function validateCard(card: CardState): boolean {
  if (htmlIsEmpty(card.draft.title)) {
    message.warning(
      $t("plugin.linapro-eval-360.page.questionnaire.titleRequired"),
    );
    return false;
  }
  // 开启「填写描述」后描述即为必填。
  if (card.showDescription && htmlIsEmpty(card.draft.description)) {
    message.warning(
      $t("plugin.linapro-eval-360.page.questionnaire.descriptionRequired"),
    );
    return false;
  }
  if (card.draft.type === "single_choice" && card.draft.options.length === 0) {
    message.warning(
      $t("plugin.linapro-eval-360.page.questionnaire.optionsRequired"),
    );
    return false;
  }
  if (card.draft.type === "matrix" && card.draft.matrixColumns.length === 0) {
    message.warning(
      $t("plugin.linapro-eval-360.page.questionnaire.matrixRequired"),
    );
    return false;
  }
  return true;
}

function buildInput(card: CardState, sort: number): QuestionSaveInput {
  const options = card.draft.options.map((o, i) => ({
    ...o,
    sort: o.sort || i,
  }));
  const matrixColumns = card.draft.matrixColumns.map((c, i) => ({
    ...c,
    sort: c.sort || i,
  }));
  return {
    questionnaireId: props.questionnaire.id,
    dimensionId: props.dimensionId,
    type: card.draft.type,
    title: card.draft.title,
    description: card.showDescription ? card.draft.description : "",
    required: card.draft.required,
    sort,
    options,
    matrixColumns,
    visibleRoleIds: card.draft.visibleRoleIds,
    excludedEvaluatorEmpIds: card.draft.excludedEvaluatorEmpIds,
  };
}

function isDirty(card: CardState): boolean {
  return snapshot(card.draft) !== card.saved;
}

// 供父级「完成编辑」前确认：是否存在填过内容但未保存的题目卡片（空白草稿卡不算）。
defineExpose({ hasUnsaved: () => cards.value.some((c) => isDirty(c)) });

// startEditCard 进入编辑态，显示题型/选项/开关等配置控件。
function startEditCard(card: CardState) {
  card.editing = true;
}

// insertAfterCard 在某道已存题目之后插入一张新的空白草稿卡（进入编辑态）。
function insertAfterCard(card: CardState) {
  const idx = cards.value.findIndex((c) => c.key === card.key);
  if (idx < 0) return;
  cards.value.splice(idx + 1, 0, makeDraftCard(card.id));
}

// addQuestionAtEnd 在题库末尾追加一张空白草稿卡（进入编辑态）。
// 锚点为 0：保存后由父组件追加到末尾；取消则整张移除。
function addQuestionAtEnd() {
  cards.value.push(makeDraftCard(0));
}

// cancelEditCard 放弃本次编辑：已存卡从 live 题目重新拷贝草稿并退出编辑态；
// 草稿卡则整张移除。
function cancelEditCard(card: CardState) {
  if (card.id > 0) {
    const q = dimension.value?.questions.find((qq) => qq.id === card.id);
    if (q) {
      card.draft = cloneDraft(q);
      card.saved = snapshot(card.draft);
      card.showDescription = !htmlIsEmpty(q.description ?? "");
    }
    card.editing = false;
    return;
  }
  const idx = cards.value.findIndex((c) => c.key === card.key);
  if (idx >= 0) cards.value.splice(idx, 1);
}

// insertIndexFor 计算草稿卡保存后应处的目标下标（已存题按 live 顺序计数）。
// 返回 undefined 表示直接追加到末尾（无需重排）。
function insertIndexFor(card: CardState): number | undefined {
  if (card.id > 0) return undefined;
  if (card.insertAfter === 0) return undefined;
  const questions = dimension.value?.questions ?? [];
  const anchorIdx = questions.findIndex((q) => q.id === card.insertAfter);
  // 锚点题已不存在（被并发删除）：退化为追加。
  if (anchorIdx < 0) return undefined;
  return anchorIdx + 1;
}

// saveCard 保存单张卡片；新增卡保存成功后由父组件 refetch + syncCards 转为正式卡（只读态）。
// 若草稿卡锚定在中间位置，则把目标下标一并交给父组件，由其在落库后调用重排接口整体顺延。
async function saveCard(card: CardState) {
  if (!validateCard(card)) return;
  const target = insertIndexFor(card);
  const sort =
    card.id > 0
      ? (dimension.value?.questions.findIndex((q) => q.id === card.id) ?? 0)
      : (target ?? dimension.value?.questions.length ?? 0);
  const input = buildInput(card, sort < 0 ? 0 : sort);
  card.saving = true;
  try {
    // 保存失败（校验/网络）时 saveHandler 会 reject：保持编辑态与草稿不动，由响应拦截器统一提示。
    await props.saveHandler?.(input, card.id > 0 ? card.id : undefined, target);
    // 回填基线：即便父组件尚未刷新，重复点击也不会重复提交。
    card.saved = snapshot(card.draft);
    // 保存后回到只读态，隐藏配置控件。
    card.editing = false;
    // 草稿卡落库后由后端返回的正式卡取代：整张移除，
    // 否则 syncCards 会按锚点再插回来，与 refetch 到的正式卡重复。
    if (card.id === 0) {
      const idx = cards.value.findIndex((c) => c.key === card.key);
      if (idx >= 0) cards.value.splice(idx, 1);
    }
  } catch {
    // 落库失败：题目并未产生，草稿保持原样，用户可直接重试。
  } finally {
    card.saving = false;
  }
}
</script>

<template>
  <div class="flex flex-col" data-testid="eval360-question-editor">
    <!-- 一题一卡片、一题一横线分隔。已存卡片默认只读，hover 右下角出现「编辑」按钮进入编辑态。 -->
    <template v-for="(card, cardIndex) in cards" :key="card.key">
      <div
        class="group relative flex flex-col gap-3 py-8"
        data-testid="eval360-question-item"
      >
        <!-- 题号 + 题干（编辑态为富文本输入，只读态为渲染文本） -->
        <div class="flex items-start gap-2">
          <!-- 编辑态题干是带边框内边距的富文本编辑器，题号需下移对齐其首行文字；只读态两者都是纯文本，顶对齐即可。 -->
          <span
            class="text-sm font-semibold text-gray-500"
            :class="card.editing ? 'pt-2' : undefined"
          >
            {{ dimensionBaseOffset + Number(cardIndex) + 1 }}.
          </span>
          <div class="min-w-0 flex-1">
            <div
              v-if="card.editing"
              class="eval360-rte eval360-rte-single eval360-rte-title"
              :data-testid="
                card.id > 0
                  ? 'eval360-question-title'
                  : 'eval360-question-new-title'
              "
            >
              <TiptapEditor
                v-model="card.draft.title"
                :height="32"
                :max-height="120"
                default-bold
                :placeholder="
                  $t(
                    'plugin.linapro-eval-360.page.questionnaire.questionTitlePlaceholder',
                  )
                "
              />
            </div>
            <div v-else class="eval360-rich-inline text-sm font-semibold">
              <span v-html="card.draft.title"></span>
              <span v-if="card.draft.required === 1" class="text-red-500"
                >*</span
              >
            </div>
            <!-- 只读态展示描述（若有）。 -->
            <div
              v-if="!card.editing && !htmlIsEmpty(card.draft.description)"
              class="eval360-rich-block mt-1 text-xs text-gray-400"
              v-html="card.draft.description"
            ></div>
          </div>
        </div>

        <!-- 编辑态：完整配置表单；只读态：仅展示选项/矩阵结构。 -->
        <template v-if="card.editing">
          <Form layout="vertical">
            <FormItem
              :label="
                $t('plugin.linapro-eval-360.page.questionnaire.fields.type')
              "
            >
              <Select
                v-model:value="card.draft.type"
                class="max-w-[220px]"
                data-testid="eval360-question-type"
                :options="typeOptions"
              />
            </FormItem>
            <!-- 描述输入框紧随标题之下，仅在「填写描述」开关开启时显示；开启后即为必填。不再显示上方 label 提示。 -->
            <FormItem v-if="card.showDescription">
              <div
                class="eval360-rte eval360-rte-single"
                data-testid="eval360-question-desc"
              >
                <TiptapEditor
                  v-model="card.draft.description"
                  :height="32"
                  :max-height="120"
                  :placeholder="
                    $t(
                      'plugin.linapro-eval-360.page.questionnaire.questionDescPlaceholder',
                    )
                  "
                />
              </div>
            </FormItem>

            <!-- 单选选项 -->
            <template v-if="card.draft.type === 'single_choice'">
              <FormItem
                :label="
                  $t(
                    'plugin.linapro-eval-360.page.questionnaire.fields.options',
                  )
                "
              >
                <SingleChoiceOptionEditor :options="card.draft.options" />
              </FormItem>
            </template>

            <!-- 矩阵：列=评价项（经弹窗编辑）；行=被评估人（只读，来自评价关系）。
                 未导入评价关系时行渲染为占位行，仍可配置列。 -->
            <template v-if="card.draft.type === 'matrix'">
              <FormItem
                :label="
                  $t(
                    `plugin.linapro-eval-360.page.questionnaire.${matrixRowsHintKey}`,
                  )
                "
              >
                <div class="mb-2 flex justify-end">
                  <a-button
                    size="small"
                    data-testid="eval360-matrix-edit-columns"
                    @click="openColumnModal(card)"
                  >
                    {{
                      $t(
                        "plugin.linapro-eval-360.page.questionnaire.editColumns",
                      )
                    }}
                  </a-button>
                </div>
                <!-- 编辑态：列头带「⋮」操作栏（删除本列 / 允许评语 / 左移 / 右移）。 -->
                <MatrixTable
                  editable
                  :columns="card.draft.matrixColumns"
                  :rows="matrixRows"
                  :empty-hint="
                    $t(
                      'plugin.linapro-eval-360.page.questionnaire.matrixNoEvaluatee',
                    )
                  "
                  @delete-column="(i) => deleteMatrixColumn(card, i)"
                  @move-column="(i, dir) => moveMatrixColumn(card, i, dir)"
                  @toggle-allow-comment="(i) => toggleAllowComment(card, i)"
                  @toggle-comment-required="
                    (i) => toggleCommentRequired(card, i)
                  "
                  @edit-comment-placeholder="
                    (i) => openCommentModal(card, i, 'placeholder')
                  "
                  @edit-comment-presets="
                    (i) => openCommentModal(card, i, 'presets')
                  "
                />
              </FormItem>
            </template>

            <!-- 二维可见性：可见角色（评价者角色白名单）+ 受限评价者（按评价者本人的排除名单，全局生效）。
                 选项 value 是数字 id、label 是名称，需显式 option-filter-prop="label"
                 才能按名称搜索（默认按 value 过滤会导致输入名称搜不到）。
                 可见角色变化时清理不再合法的受限评价者（即不在新选项集中的 EmpID）。 -->
            <FormItem
              :label="
                $t(
                  'plugin.linapro-eval-360.page.questionnaire.fields.visibleRoles',
                )
              "
            >
              <Select
                v-model:value="card.draft.visibleRoleIds"
                mode="multiple"
                allow-clear
                show-search
                option-filter-prop="label"
                :options="roleOptions"
                :placeholder="
                  $t(
                    'plugin.linapro-eval-360.page.questionnaire.visibleRolesPlaceholder',
                  )
                "
                @change="onVisibleRolesChange(card)"
              />
            </FormItem>
            <FormItem
              :label="
                $t(
                  'plugin.linapro-eval-360.page.questionnaire.fields.excludedEvaluators',
                )
              "
            >
              <Select
                v-model:value="card.draft.excludedEvaluatorEmpIds"
                mode="multiple"
                allow-clear
                show-search
                option-filter-prop="label"
                :options="evaluatorOptionsFor(card)"
                :placeholder="
                  $t(
                    'plugin.linapro-eval-360.page.questionnaire.excludedEvaluatorsPlaceholder',
                  )
                "
                data-testid="eval360-question-excluded-evaluators"
              />
            </FormItem>
          </Form>

          <!-- 编辑态底部：必填开关 + 填写描述开关 + 保存/取消/删除。 -->
          <div class="flex items-center gap-6">
            <span class="flex items-center gap-2">
              <Switch
                :checked="card.draft.required === 1"
                data-testid="eval360-question-required"
                @change="(v: boolean) => (card.draft.required = v ? 1 : 0)"
              />
              <span class="text-sm">{{
                $t("plugin.linapro-eval-360.page.questionnaire.fields.required")
              }}</span>
            </span>
            <span class="flex items-center gap-2">
              <Switch
                v-model:checked="card.showDescription"
                data-testid="eval360-question-has-desc"
              />
              <span class="text-sm">{{
                $t("plugin.linapro-eval-360.page.questionnaire.fillDescription")
              }}</span>
            </span>
          </div>

          <div class="flex justify-end gap-2">
            <a-button size="small" @click="cancelEditCard(card)">
              {{ $t("plugin.linapro-eval-360.page.common.cancel") }}
            </a-button>
            <a-button
              v-if="card.id > 0"
              danger
              size="small"
              @click="emit('delete', card.id)"
            >
              {{ $t("plugin.linapro-eval-360.page.common.delete") }}
            </a-button>
            <a-button
              type="primary"
              size="small"
              :loading="card.saving"
              :data-testid="
                card.id > 0
                  ? 'eval360-question-save'
                  : 'eval360-question-create'
              "
              @click="saveCard(card)"
            >
              {{
                card.id > 0
                  ? $t("plugin.linapro-eval-360.page.common.save")
                  : $t(
                      "plugin.linapro-eval-360.page.questionnaire.createQuestion",
                    )
              }}
            </a-button>
          </div>
        </template>

        <!-- 只读态：展示选项；矩阵结构由上方预览区块承担（已在其 FormItem 内）。 -->
        <template v-else>
          <template v-if="card.draft.type === 'single_choice'">
            <SingleChoiceOptionEditor :options="card.draft.options" readonly />
          </template>
          <div
            v-if="card.draft.type === 'matrix'"
            data-testid="eval360-question-matrix-preview"
          >
            <MatrixTable
              :columns="card.draft.matrixColumns"
              :rows="matrixRows"
              :empty-hint="
                $t(
                  'plugin.linapro-eval-360.page.questionnaire.matrixNoEvaluatee',
                )
              "
            />
          </div>
          <Tag v-if="isDirty(card)" color="orange" size="small">
            {{ $t("plugin.linapro-eval-360.page.questionnaire.unsaved") }}
          </Tag>
        </template>

        <!-- 已存且非编辑态：hover 时右下角浮出「编辑」与「在此后插入」按钮。 -->
        <div
          v-if="card.id > 0 && !card.editing"
          class="!absolute bottom-2 right-2 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100"
        >
          <a-button
            type="link"
            size="small"
            data-testid="eval360-question-insert-after"
            @click="insertAfterCard(card)"
          >
            {{ $t("plugin.linapro-eval-360.page.questionnaire.insertAfter") }}
          </a-button>
          <a-button
            type="link"
            size="small"
            data-testid="eval360-question-edit"
            @click="startEditCard(card)"
          >
            {{ $t("plugin.linapro-eval-360.page.common.edit") }}
          </a-button>
        </div>
      </div>
      <Divider v-if="cardIndex < cards.length - 1" class="!my-0" />
    </template>

    <!-- 底部新增题目入口：点按在末尾追加一张空白草稿卡（编辑态）；
         取消即整张移除，保存后转为正式题目。 -->
    <div class="py-6">
      <a-button
        type="dashed"
        block
        data-testid="eval360-question-add"
        @click="addQuestionAtEnd"
      >
        + {{ $t("plugin.linapro-eval-360.page.questionnaire.createQuestion") }}
      </a-button>
    </div>

    <!-- 评价项编辑弹窗：列编辑逻辑见 MatrixColumnsModal。 -->
    <MatrixColumnsModal
      v-model:open="columnModalOpen"
      :columns="columnTargetCard?.draft.matrixColumns ?? []"
      @submit="submitColumnModal"
    />

    <!-- 列级评语配置弹窗：提示语 / 预设填空两个子项共用。 -->
    <MatrixColumnCommentModal
      v-model:open="commentModalOpen"
      :mode="commentModalMode"
      :column-label="commentTargetColumn?.label ?? ''"
      :placeholder="commentTargetColumn?.commentPlaceholder ?? ''"
      :presets="commentTargetColumn?.commentPresets ?? []"
      :allow-custom="commentTargetColumn?.commentAllowCustom ?? 0"
      @submit="submitCommentModal"
    />
  </div>
</template>

<style scoped>
/* 只读态标题为富文本（HTML）：段落内联渲染，避免 <p> 的块级换行与边距。 */
.eval360-rich-inline :deep(p) {
  display: inline;
  margin: 0;
}

/* 只读态描述为富文本块：保留段落形态。 */
.eval360-rich-block :deep(p) {
  margin: 0;
}

/* 富文本编辑器工具栏：默认隐藏，聚焦时以浮层形式出现在输入框上方——
   绝对定位 + overflow: visible，让它脱离文档流，出现/消失都不会顶动下方文字（不会引起页面跳动）。 */
.eval360-rte {
  position: relative;
}
.eval360-rte :deep(.tiptap-toolbar) {
  position: absolute;
  bottom: 100%;
  left: 0;
  z-index: 20;
  display: none;
  margin-bottom: 4px;
  background: #fff;
  border: 1px solid #d9d9d9;
  border-radius: 6px;
  box-shadow: 0 2px 8px rgb(0 0 0 / 10%);
}
.eval360-rte:focus-within :deep(.tiptap-toolbar) {
  display: block;
}

/* 题干/描述编辑器默认不画边框，看起来就是一段文字；聚焦时才浮出输入框边框与高亮。 */
.eval360-rte :deep(.tiptap-editor) {
  overflow: visible; /* 关键：不裁剪上方的工具栏浮层 */
  border-color: transparent;
  box-shadow: none;
}
.eval360-rte:focus-within :deep(.tiptap-editor) {
  border-color: #1677ff;
  box-shadow: 0 0 0 2px rgb(5 145 255 / 10%);
}

/* 标题为单行：收紧内边距贴近普通输入框高度，段落内联不换行撑高。 */
.eval360-rte-single :deep(.tiptap-content) {
  padding: 5px 11px;
}
.eval360-rte-single :deep(.tiptap-content .tiptap > p) {
  margin: 0;
}
</style>
