<script setup lang="ts">
import { computed, h, onMounted, ref } from "vue";

import { useVbenModal } from "@vben/common-ui";

import {
  Card,
  Input,
  InputNumber,
  message,
  Modal,
  Switch,
  Table,
  Tag,
  Upload,
} from "ant-design-vue";

import { $t } from "#/locales";
import { downloadBlob } from "#/utils/download";
import {
  relationExport,
  relationImport,
  relationImportTemplate,
  relationList,
  evaluateeDelete,
  roleDelete,
  roleRename,
  roleUpdateWeight,
  type EvaluateeRow,
  type RelationItem,
  type RelationMatrix,
  type RoleGroup,
  type RoleItem,
} from "../eval-client";
import RelationBatchAddModal from "./relation-batch-add-modal.vue";
import RelationEditModal from "./relation-edit-modal.vue";

const props = defineProps<{
  questionnaireId: number;
  published: number;
  roles: RoleItem[];
}>();
const emit = defineEmits<{ changed: [] }>();

const matrix = ref<RelationMatrix | null>(null);
const loading = ref(false);

// 角色权重、全量导入分别改为弹窗承载（由关系明细右上角按钮触发）。
const weightModalOpen = ref(false);
const importModalOpen = ref(false);

const [BatchAddModal, batchAddModalApi] = useVbenModal({
  connectedComponent: RelationBatchAddModal,
});
const [EditModal, editModalApi] = useVbenModal({
  connectedComponent: RelationEditModal,
});

async function refresh() {
  loading.value = true;
  try {
    matrix.value = await relationList(props.questionnaireId);
  } finally {
    loading.value = false;
  }
}

onMounted(refresh);

const canImport = computed(() => props.published !== 1);

// ---- 角色权重 ----
// 本地可编辑快照：打开弹窗时从 props.roles 初始化（名称 + 权重），点「保存」才统一落库
// （改名不再二次弹窗，直接在弹窗内编辑；也不再失去光标即保存）。
const localWeights = ref<Record<number, number>>({});
const localNames = ref<Record<number, string>>({});

function openWeightModal() {
  const w: Record<number, number> = {};
  const n: Record<number, string> = {};
  for (const r of props.roles) {
    w[r.id] = Number(r.weight || 0);
    n[r.id] = r.name;
  }
  localWeights.value = w;
  localNames.value = n;
  weightModalOpen.value = true;
}

async function saveRoles() {
  // 逐个保存：名称有改动且非空时先改名，再落权重；顺序执行便于错误定位。
  for (const r of props.roles) {
    const newName = (localNames.value[r.id] ?? "").trim();
    if (newName && newName !== r.name) {
      await roleRename(r.id, newName);
    }
    await roleUpdateWeight(r.id, String(localWeights.value[r.id] ?? 0));
  }
  message.success($t("plugin.linapro-eval-360.page.common.updateSuccess"));
  weightModalOpen.value = false;
  emit("changed");
}

// 删除角色（问卷级）：与关系编辑弹窗的垃圾桶按钮同语义，连带删除该角色在**全部被评估人**
// 名下的评价关系，故二次确认文案也必须点明这一影响范围，避免用户以为只删空角色。
// 该角色下已有作答时后端拒绝，前端只透传错误提示。
async function deleteRole(role: RoleItem) {
  Modal.confirm({
    title: $t("plugin.linapro-eval-360.page.role.deleteConfirmTitle", {
      role: role.name,
    }),
    content: $t("plugin.linapro-eval-360.page.role.deleteConfirmContent"),
    okType: "danger",
    onOk: async () => {
      await roleDelete(role.id, true);
      message.success($t("plugin.linapro-eval-360.page.common.deleteSuccess"));
      delete localWeights.value[role.id];
      await refresh();
      emit("changed");
    },
  });
}
// ---- Excel 导入/导出 ----
async function downloadTemplate() {
  const blob = await relationImportTemplate();
  downloadBlob(blob, "eval360-relation-import-template.xlsx");
}

async function downloadExport() {
  const blob = await relationExport(props.questionnaireId);
  downloadBlob(blob, "评价关系导出.xlsx");
}

const UploadDragger = Upload.Dragger;
const importFileList = ref<any[]>([]);
// 默认覆盖式全量导入（后端本就是全量替换语义）。
const overwrite = ref(true);

async function handleImport(file: File) {
  try {
    const result = await relationImport(props.questionnaireId, file);
    const content = `${$t(
      "plugin.linapro-eval-360.page.relation.importSummary",
      {
        success: result.success,
        fail: result.fail,
      },
    )}${
      result.fail > 0
        ? "\n" +
          result.failList
            .slice(0, 5)
            .map(
              (item) =>
                `${$t("plugin.linapro-eval-360.page.relation.rowReason", {
                  row: item.row,
                  reason: item.reason,
                })}`,
            )
            .join("\n") +
          (result.failList.length > 5 ? "\n..." : "")
        : ""
    }`;
    if (result.fail > 0) {
      Modal.error({
        title: $t("plugin.linapro-eval-360.page.relation.importResultTitle"),
        content: () =>
          h(
            "div",
            { class: "max-h-[260px] overflow-y-auto whitespace-pre-wrap" },
            content,
          ),
      });
    } else {
      message.success(
        $t("plugin.linapro-eval-360.page.relation.importSuccess", {
          count: result.success,
        }),
      );
    }
    await refresh();
    emit("changed");
  } finally {
    importFileList.value = [];
  }
}

function beforeUpload(file: File) {
  handleImport(file);
  return false; // 阻止 antd 自动上传
}

// ---- 关系编辑 ----
// 工具栏「新增评价关系」：五栏按行录入（被评估人姓名/职务/部门、评价者姓名、角色），不录入工号——
// 后端按在职员工姓名匹配，任一姓名在职重名或找不到时整批拒绝并点名。
// 角色为自由文本，后端按名称 upsert，不依赖预先配置的角色。
function openAdd() {
  batchAddModalApi.setData({
    questionnaireId: props.questionnaireId,
  });
  batchAddModalApi.open();
}

// 角色列的「编辑」按钮：只针对「当前被评估人 + 当前角色」这一个单元格，
// 不涉及其它角色的关系。
// 角色取自矩阵（与角色列同源）：新增弹窗按名称 upsert 出的新角色只在矩阵刷新后可见，
// 若从 props.roles 取会因父组件尚未刷新而落空，导致提交时角色名为空。
function openEdit(row: EvaluateeRow, roleId: number) {
  const role = (matrix.value?.roles ?? []).find((r) => r.id === roleId);
  editModalApi.setData({
    row,
    role,
    questionnaireId: props.questionnaireId,
  });
  editModalApi.open();
}

// 角色列的「新增」按钮：复用批量录入弹窗，预填当前行被评估人的姓名/职务/部门，
// 用户只需补录评价人即可，无需重复输入被评估人。
function openAddForEvaluatee(row: EvaluateeRow) {
  batchAddModalApi.setData({
    questionnaireId: props.questionnaireId,
    evaluateeName: row.evaluateeName,
    evaluateeJobTitle: row.evaluateeJobTitle,
    evaluateeDepartment: row.evaluateeDepartment,
  });
  batchAddModalApi.open();
}

// 新增/编辑弹窗保存后：关系增删可能新建角色或被评估人，除刷新矩阵外还需通知父组件
// 重拉问卷详情，保证角色权重弹窗等依赖 props.roles 的入口拿到最新角色。
async function onRelationsSaved() {
  await refresh();
  emit("changed");
}

// 取某被评估人在指定角色下的评价人分组。
function groupOf(record: any, roleId: number): RoleGroup | undefined {
  return (record.groups ?? []).find((g: RoleGroup) => g.roleId === roleId);
}

// 被评估人列的「删除」按钮：删除当前被评估人及其全部评价关系（后端在单事务内连带清理）。
// 其下任一评价关系已有作答时后端拒绝，二次确认后再调用。
function deleteEvaluatee(row: EvaluateeRow) {
  Modal.confirm({
    title: $t(
      "plugin.linapro-eval-360.page.relation.deleteEvaluateeConfirmTitle",
    ),
    okType: "danger",
    onOk: async () => {
      await evaluateeDelete(row.evaluateeId);
      message.success($t("plugin.linapro-eval-360.page.common.deleteSuccess"));
      await refresh();
      emit("changed");
    },
  });
}

// 按完成状态拆分：已评价（filled）/ 未评价（pending|voided）。
function splitByFilled(group?: RoleGroup) {
  const items = group?.items ?? [];
  return {
    filled: items.filter(
      (x) => x.completed === 1 || x.derivedStatus === "filled",
    ),
    pending: items.filter(
      (x) => !(x.completed === 1 || x.derivedStatus === "filled"),
    ),
  };
}

// 评价人展示文本：名字(工号)，同一角色多人时一人一行。
function evaluatorLabel(item: RelationItem) {
  return `${item.evaluatorName}(${item.evaluatorNo})`;
}

const weightSum = computed(() => {
  const sum = props.roles.reduce(
    (acc, r) => acc + Number(localWeights.value[r.id] ?? r.weight ?? 0),
    0,
  );
  return sum;
});

// 关系明细列：被评估人 + 职务 + 部门 + 各角色（动态）。
// 注意：这里用的是 ant-design-vue 的 Table，列配置必须用 dataIndex/key，
// 单元格收敛到 #bodyCell 插槽渲染（vxe-grid 的 field/slots/params 在这里会被忽略，导致整表空白）。
const matrixColumns = computed(() => {
  const base = [
    {
      dataIndex: "evaluateeName",
      key: "evaluateeName",
      title: $t("plugin.linapro-eval-360.page.relation.evaluatee"),
      width: 140,
    },
    {
      dataIndex: "evaluateeJobTitle",
      key: "evaluateeJobTitle",
      title: $t("plugin.linapro-eval-360.page.relation.evaluateeJobTitle"),
      width: 140,
    },
    {
      dataIndex: "evaluateeDepartment",
      key: "evaluateeDepartment",
      title: $t("plugin.linapro-eval-360.page.relation.evaluateeDepartment"),
      width: 160,
    },
  ];
  const roleCols = (matrix.value?.roles ?? []).map((role) => ({
    dataIndex: `role_${role.id}`,
    key: `role_${role.id}`,
    title: role.name,
    width: 220,
    // 自定义字段：在 #bodyCell 里按角色分组迭代用。
    roleId: role.id,
  }));
  // 末列操作栏：承载「新增 / 删除」按钮（真正渲染靠 #bodyCell 的 actions 分支）。
  const actionCol = {
    dataIndex: "actions",
    key: "actions",
    title: $t("plugin.linapro-eval-360.page.common.operation"),
    width: 130,
    align: "center" as const,
  };
  return [...base, ...roleCols, actionCol];
});

// 角色列在表格里没有真实取值，dataIndex 仅用于生成列；真正渲染靠下面的 #bodyCell。
function roleIdOf(column: any): number | undefined {
  return column?.roleId;
}

const matrixData = computed(() => matrix.value?.evaluatees ?? []);
</script>

<template>
  <div class="flex flex-col gap-4" data-testid="eval360-relation-panel">
    <!-- 关系明细表：角色权重 / 导入 / 导出 均收敛到右上角工具栏。 -->
    <Card
      size="small"
      :title="$t('plugin.linapro-eval-360.page.relation.matrixTitle')"
    >
      <template #extra>
        <div class="flex flex-wrap items-center gap-2">
          <a-button size="small" @click="openWeightModal">
            {{ $t("plugin.linapro-eval-360.page.role.weightsTitle") }}
          </a-button>
          <a-button
            size="small"
            data-testid="eval360-relation-add-btn"
            @click="openAdd"
          >
            {{ $t("plugin.linapro-eval-360.page.relation.addRelation") }}
          </a-button>
          <a-button
            size="small"
            data-testid="eval360-relation-import-btn"
            :disabled="!canImport"
            @click="importModalOpen = true"
          >
            {{ $t("plugin.linapro-eval-360.page.relation.import") }}
          </a-button>
          <a-button
            size="small"
            data-testid="eval360-relation-export-btn"
            @click="downloadExport"
          >
            {{ $t("plugin.linapro-eval-360.page.relation.export") }}
          </a-button>
        </div>
      </template>
      <Table
        data-testid="eval360-relation-matrix"
        class="eval360-relation-matrix"
        bordered
        :loading="loading"
        :data-source="matrixData"
        :columns="matrixColumns"
        row-key="evaluateeId"
        :pagination="{ pageSize: 10, showSizeChanger: true }"
      >
        <!-- 角色列统一走 #bodyCell（antd 的插槽名）：左为评价人明细，右为「编辑」按钮，
             同一角色多人一人一行，已评价 / 未评价分组展示。 -->
        <template #bodyCell="{ record, column }">
          <!-- 职务/部门为可选录入：未填写时显示占位符，与空角色单元格一致。 -->
          <template
            v-if="
              column.key === 'evaluateeJobTitle' ||
              column.key === 'evaluateeDepartment'
            "
          >
            <span
              v-if="record[column.key]"
              class="break-all"
              :data-testid="`eval360-relation-${column.key}`"
            >
              {{ record[column.key] }}
            </span>
            <span v-else class="text-gray-400">—</span>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="flex items-center justify-center gap-4">
              <a-button
                type="link"
                size="small"
                class="!px-0"
                data-testid="eval360-evaluatee-add-btn"
                @click="openAddForEvaluatee(record)"
              >
                {{ $t("plugin.linapro-eval-360.page.common.add") }}
              </a-button>
              <a-button
                type="link"
                danger
                size="small"
                class="!px-0"
                data-testid="eval360-evaluatee-delete-btn"
                @click="deleteEvaluatee(record)"
              >
                {{ $t("plugin.linapro-eval-360.page.common.delete") }}
              </a-button>
            </div>
          </template>
          <template v-else-if="roleIdOf(column) !== undefined">
            <div class="flex items-start justify-between gap-2 text-sm">
              <div class="flex min-w-0 flex-1 flex-col gap-1">
                <template v-if="groupOf(record, roleIdOf(column)!)">
                  <div
                    v-if="
                      splitByFilled(groupOf(record, roleIdOf(column)!)).filled
                        .length
                    "
                    class="flex items-start gap-1"
                  >
                    <Tag color="success" class="shrink-0">
                      {{
                        $t(
                          "plugin.linapro-eval-360.page.relation.status.filled",
                        )
                      }}
                    </Tag>
                    <!-- 评价人列固定单行（whitespace-nowrap），列宽不足时只有这块横向滚动。
                         w-max 让内层列宽贴合最长的一条「姓名(工号)」，故单位是「条」而非「字」，
                         配合 flex-col 恰好是「一人一行 + 短名左对齐」。 -->
                    <div
                      :data-testid="`eval360-role-cell-scroll-${roleIdOf(column)}-filled`"
                      class="min-w-0 flex-1 overflow-x-auto pb-0.5"
                    >
                      <div class="flex w-max flex-col">
                        <span
                          v-for="item in splitByFilled(
                            groupOf(record, roleIdOf(column)!),
                          ).filled"
                          :key="item.id"
                          class="whitespace-nowrap"
                        >
                          {{ evaluatorLabel(item) }}
                        </span>
                      </div>
                    </div>
                  </div>
                  <div
                    v-if="
                      splitByFilled(groupOf(record, roleIdOf(column)!)).pending
                        .length
                    "
                    class="flex items-start gap-1"
                  >
                    <Tag class="shrink-0">
                      {{
                        $t(
                          "plugin.linapro-eval-360.page.relation.status.pending",
                        )
                      }}
                    </Tag>
                    <!-- 未评价分组与已评价分组同结构，各自独立横向滚动（两组标签宽度不同）。 -->
                    <div
                      :data-testid="`eval360-role-cell-scroll-${roleIdOf(column)}-pending`"
                      class="min-w-0 flex-1 overflow-x-auto pb-0.5"
                    >
                      <div class="flex w-max flex-col">
                        <span
                          v-for="item in splitByFilled(
                            groupOf(record, roleIdOf(column)!),
                          ).pending"
                          :key="item.id"
                          class="whitespace-nowrap"
                        >
                          {{ evaluatorLabel(item) }}
                        </span>
                      </div>
                    </div>
                  </div>
                </template>
                <span v-else class="text-gray-400">—</span>
              </div>
              <div class="flex shrink-0 items-center gap-2">
                <a-button
                  type="link"
                  size="small"
                  class="shrink-0 !px-0"
                  data-testid="eval360-role-edit-btn"
                  @click="openEdit(record, roleIdOf(column)!)"
                >
                  {{ $t("plugin.linapro-eval-360.page.common.edit") }}
                </a-button>
              </div>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <!-- 角色权重弹窗 -->
    <Modal
      v-model:open="weightModalOpen"
      :title="$t('plugin.linapro-eval-360.page.role.weightsTitle')"
      :footer="null"
      width="520px"
    >
      <div class="flex flex-col gap-3 py-2">
        <div v-for="r in roles" :key="r.id" class="flex items-center gap-3">
          <Input
            v-model:value="localNames[r.id]"
            class="flex-1"
            :maxlength="50"
            data-testid="eval360-role-name"
          />
          <InputNumber
            v-model:value="localWeights[r.id]"
            class="eval360-role-weight"
            data-testid="eval360-role-weight"
            style="width: 110px"
            :min="0"
            :max="100"
            :step="1"
            addon-after="%"
          />
          <a-button type="link" danger size="small" @click="deleteRole(r)">
            {{ $t("plugin.linapro-eval-360.page.common.delete") }}
          </a-button>
        </div>
        <div
          v-if="roles.length === 0"
          class="py-4 text-center text-sm text-gray-400"
        >
          {{ $t("plugin.linapro-eval-360.page.role.empty") }}
        </div>
        <div class="mt-1 flex items-center justify-between border-t pt-3">
          <span class="text-sm">
            {{ $t("plugin.linapro-eval-360.page.role.weightSum") }}:
            <span
              :class="weightSum === 100 ? 'text-green-600' : 'text-red-500'"
            >
              {{ weightSum }}%
            </span>
            <span v-if="weightSum !== 100" class="ml-2 text-xs text-red-400">
              {{ $t("plugin.linapro-eval-360.page.role.weightSumHint") }}
            </span>
          </span>
          <a-button
            type="primary"
            data-testid="eval360-role-weight-save"
            :disabled="roles.length === 0"
            @click="saveRoles"
          >
            {{ $t("plugin.linapro-eval-360.page.common.save") }}
          </a-button>
        </div>
      </div>
    </Modal>

    <!-- 全量导入弹窗 -->
    <Modal
      v-model:open="importModalOpen"
      :title="$t('plugin.linapro-eval-360.page.relation.import')"
      :footer="null"
      width="560px"
    >
      <div class="flex flex-col gap-3 py-2">
        <UploadDragger
          data-testid="eval360-relation-import-upload"
          v-model:file-list="importFileList"
          :before-upload="beforeUpload"
          :max-count="1"
          accept=".xlsx,.xls"
          :disabled="!canImport"
        >
          <p class="ant-upload-text">
            {{ $t("plugin.linapro-eval-360.page.relation.import") }}
          </p>
          <p class="ant-upload-hint text-xs text-gray-400">
            {{ $t("plugin.linapro-eval-360.page.relation.importHint") }}
          </p>
        </UploadDragger>
        <div class="flex flex-wrap items-center gap-2">
          <a-button @click="downloadTemplate">
            {{ $t("plugin.linapro-eval-360.page.relation.downloadTemplate") }}
          </a-button>
          <span class="ml-auto flex items-center gap-2 text-sm">
            <span :class="{ 'text-red-500': overwrite }">
              {{ $t("plugin.linapro-eval-360.page.relation.overwrite") }}
            </span>
            <Switch v-model:checked="overwrite" :disabled="!canImport" />
          </span>
        </div>
        <span
          v-if="!canImport"
          data-testid="eval360-relation-import-disabled"
          class="text-xs text-red-400"
        >
          {{ $t("plugin.linapro-eval-360.page.relation.importDisabledHint") }}
        </span>
      </div>
    </Modal>

    <BatchAddModal @reload="onRelationsSaved" />
    <EditModal @reload="onRelationsSaved" />
  </div>
</template>

<style scoped>
/* 用 antd 的 bordered 画列竖线（真边框，能连续贯穿每一行），
   再把所有横向线去掉，只保留竖线，效果即「每列独立、竖线贯穿到底、无横线」。
   注意：Table 上只有 data-testid 没有 class，选择器必须用属性命中。 */
[data-testid="eval360-relation-matrix"] :deep(.ant-table-thead > tr > th),
[data-testid="eval360-relation-matrix"] :deep(.ant-table-tbody > tr > td) {
  border-bottom: none !important;
}

/* 表头标题（被评估人 / 各角色 / 操作）统一居中；单元格内容保持原对齐。
   antd Table 只把 class/style 落到带 scoped 属性的外层 wrapper，data-testid 被透传到内层 table，
   用 data-testid 做 scoped 选择器匹配不到，这里必须用 class 命中。 */
.eval360-relation-matrix :deep(.ant-table-thead > tr > th) {
  text-align: center !important;
}

/* 去掉表头单元格自带的短竖线，避免与 bordered 的全高竖线重叠成双线。 */
[data-testid="eval360-relation-matrix"]
  :deep(.ant-table-thead > tr > th)::before {
  display: none !important;
}
</style>
