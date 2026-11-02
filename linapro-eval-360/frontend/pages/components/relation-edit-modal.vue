<script setup lang="ts">
import { ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';

import { Button, Input, message, Modal, Tag } from 'ant-design-vue';

import { $t } from '#/locales';
import {
  relationAdd,
  relationDelete,
  roleDelete,
  type EvaluateeRow,
  type RoleItem,
} from '../eval-client';

const emit = defineEmits<{ reload: [] }>();

// 按「被评估人 + 单个角色」进入：只管理该单元格下的评价人，逐行可增可删。
// 被评估人与角色均锁定，不提供修改；需要其它角色/被评估人请从工具栏批量新增。
// 已有行只读展示「姓名(工号)」；新增行只录入姓名（后端按在职员工姓名匹配，重名/找不到时整批拒绝）。
interface EditableRow {
  // 已有关系的主键；0 表示新增行。
  id: number;
  evaluatorName: string;
  // 已有行回显用的工号；新增行为空。
  evaluatorNo: string;
}

const questionnaireId = ref<number>(0);
const evaluateeName = ref('');
const evaluateeNo = ref('');
const role = ref<RoleItem | undefined>();
const rows = ref<EditableRow[]>([]);
const submitting = ref(false);

const roleName = () => role.value?.name ?? '';

function emptyRow(): EditableRow {
  return { id: 0, evaluatorName: '', evaluatorNo: '' };
}

// 只取当前角色下的评价人，其它角色的关系不进入本弹窗。
function rowsOfRole(row: EvaluateeRow | undefined, roleId: number): EditableRow[] {
  const group = (row?.groups ?? []).find((g) => g.roleId === roleId);
  return (group?.items ?? []).map((item) => ({
    id: item.id,
    evaluatorName: item.evaluatorName,
    evaluatorNo: item.evaluatorNo,
  }));
}

// 记录打开时的原始行，保存时据此判断哪些被删除。
let originalRows: EditableRow[] = [];

const [BasicModal, modalApi] = useVbenModal({
  async onOpenChange(open) {
    if (!open) return;
    const data = modalApi.getData<{
      row?: EvaluateeRow;
      role?: RoleItem;
      questionnaireId: number;
    }>();
    questionnaireId.value = data?.questionnaireId ?? 0;
    role.value = data?.role;
    evaluateeName.value = data?.row?.evaluateeName ?? '';
    evaluateeNo.value = data?.row?.evaluateeNo ?? '';
    originalRows = rowsOfRole(data?.row, data?.role?.id ?? 0);
    // 复制一份，避免直接改动 props 层的数据。
    rows.value = originalRows.map((r) => ({ ...r }));
  },
  async onConfirm() {
    // 表单校验：新增行必须填写评价人姓名。
    for (const r of rows.value) {
      if (r.id === 0 && !r.evaluatorName.trim()) {
        message.warning(
          $t('plugin.linapro-eval-360.page.relation.evaluatorNameRequired'),
        );
        return;
      }
    }
    submitting.value = true;
    modalApi.setState({ confirmLoading: true });
    try {
      // 已有行保留；新增行一次批量提交（重名/找不到时后端整批拒绝，不会只加进去一部分）。
      const keptIds = new Set(rows.value.filter((r) => r.id > 0).map((r) => r.id));
      const items = rows.value
        .filter((r) => r.id === 0)
        .map((r) => ({
          evaluateeName: evaluateeName.value.trim(),
          evaluatorName: r.evaluatorName.trim(),
          roleName: roleName(),
        }));
      if (items.length > 0) {
        await relationAdd({ questionnaireId: questionnaireId.value, items });
      }
      // 打开时存在、但已被删除的行，逐个删除。
      for (const origin of originalRows) {
        if (!keptIds.has(origin.id)) {
          await relationDelete(origin.id);
        }
      }
      message.success($t('plugin.linapro-eval-360.page.common.operateSuccess'));
      emit('reload');
      modalApi.close();
    } finally {
      submitting.value = false;
      modalApi.setState({ confirmLoading: false });
    }
  },
});

function addRow() {
  rows.value.push(emptyRow());
}

function removeRow(index: number) {
  rows.value.splice(index, 1);
}

// 删除当前角色：角色是问卷级的，删掉它会连同各被评估人在该角色下的全部评价关系一起消失
// （后端 cascade=true 在单事务内清理）。因此二次确认里必须点明「全部被评估人」这一范围。
// 该角色下已有作答时后端拒绝，前端只透传错误提示。
// 确认文案与角色权重弹窗的删除入口共用 role.deleteConfirm*，两处语义一致、避免两套措辞漂移。
const deletingRole = ref(false);

function removeRole() {
  const current = role.value;
  if (!current) {
    return;
  }
  Modal.confirm({
    title: $t('plugin.linapro-eval-360.page.role.deleteConfirmTitle', {
      role: current.name,
    }),
    content: $t('plugin.linapro-eval-360.page.role.deleteConfirmContent'),
    okType: 'danger',
    onOk: async () => {
      deletingRole.value = true;
      try {
        await roleDelete(current.id, true);
        message.success($t('plugin.linapro-eval-360.page.common.deleteSuccess'));
        // 角色与其关系整体消失，本弹窗已无对象可编辑：通知父级刷新矩阵并关闭。
        emit('reload');
        modalApi.close();
      } finally {
        deletingRole.value = false;
      }
    },
  });
}
</script>

<template>
  <BasicModal
    :title="$t('plugin.linapro-eval-360.page.relation.editTitle')"
    class="w-[560px]"
  >
    <!-- 右上角删除当前角色。挂在标题插槽里而不是内容区：内容区是 overflow-y-auto 的
         定位祖先，按钮放那里会随内容滚动。标题行用 ml-auto 把它推到最右，
         正好落在弹窗自带的关闭（right-3）与展开（right-10）按钮一侧。 -->
    <template #title>
      <!-- pr-16 给弹窗自带的关闭（right-3）与展开（right-10）按钮让位，按钮排在展开左侧。 -->
      <span class="flex items-center gap-2 pr-16">
        {{ $t('plugin.linapro-eval-360.page.relation.editTitle') }}
        <button
          v-if="role"
          type="button"
          data-testid="eval360-edit-delete-role-btn"
          :title="$t('plugin.linapro-eval-360.page.relation.deleteRole')"
          :disabled="deletingRole"
          class="ml-auto flex-center size-5 shrink-0 rounded-full text-red-500 opacity-70 transition-opacity hover:bg-accent hover:opacity-100 disabled:pointer-events-none disabled:opacity-40"
          @click="removeRole"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="size-3.5"
            aria-hidden="true"
          >
            <path d="M3 6h18" />
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
            <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
            <line x1="10" x2="10" y1="11" y2="17" />
            <line x1="14" x2="14" y1="11" y2="17" />
          </svg>
        </button>
      </span>
    </template>

    <div class="flex flex-col gap-3">
      <!-- 被评估人 + 当前角色：本弹窗只管理这一个人在该角色下的评价人。 -->
      <div class="flex items-center gap-2 text-sm text-gray-600">
        <span>
          {{ $t('plugin.linapro-eval-360.page.relation.evaluatee') }}：
          <span class="font-medium text-gray-900">
            {{ evaluateeName }}（{{ evaluateeNo }}）
          </span>
        </span>
        <Tag color="processing" data-testid="eval360-edit-role-name">{{ roleName() }}</Tag>
      </div>

      <div
        v-for="(row, index) in rows"
        :key="row.id || `new-${index}`"
        class="flex items-center gap-2"
      >
        <!-- 已有评价人只读（改人请删掉后新增）；新增行只录入姓名。 -->
        <span
          v-if="row.id > 0"
          class="flex-1 truncate text-sm"
          data-testid="eval360-edit-evaluator-label"
        >
          {{ row.evaluatorName }}（{{ row.evaluatorNo }}）
        </span>
        <Input
          v-else
          v-model:value="row.evaluatorName"
          data-testid="eval360-edit-evaluator-name"
          :placeholder="$t('plugin.linapro-eval-360.page.relation.evaluatorName')"
        />
        <Button danger type="text" @click="removeRow(index)">
          {{ $t('plugin.linapro-eval-360.page.common.delete') }}
        </Button>
      </div>

      <div v-if="rows.length === 0" class="py-4 text-center text-sm text-gray-400">
        {{ $t('plugin.linapro-eval-360.page.relation.empty') }}
      </div>

      <Button type="dashed" block data-testid="eval360-edit-add-row" @click="addRow">
        + {{ $t('plugin.linapro-eval-360.page.common.add') }}
      </Button>
    </div>
  </BasicModal>
</template>

