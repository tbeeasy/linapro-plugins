<script setup lang="ts">
import { ref } from 'vue';

import { Empty, Input, message, Modal } from 'ant-design-vue';

import { $t } from '#/locales';
import {
  dimensionCreate,
  dimensionDelete,
  dimensionUpdate,
  questionCreate,
  questionDelete,
  questionReorder,
  questionUpdate,
  type DimensionNode,
  type EvaluateeRow,
  type QuestionnaireDetail,
  type QuestionSaveInput,
} from '../eval-client';
import QuestionEditor from './question-editor.vue';

const props = defineProps<{
  questionnaire: QuestionnaireDetail;
  // 被评估人清单（来自评价关系）：透传给题目编辑器，矩阵题的「行」由此渲染。
  evaluatees: EvaluateeRow[];
}>();
const emit = defineEmits<{ changed: [] }>();

const selectedDimensionId = ref<number | null>(
  props.questionnaire.dimensions[0]?.id ?? null,
);

// 当前维度的题目编辑器：透传其未保存状态给父级「完成编辑」确认。
const questionEditorRef = ref<InstanceType<typeof QuestionEditor> | null>(null);
defineExpose({
  hasUnsaved: () => questionEditorRef.value?.hasUnsaved() ?? false,
});

function dimensionLabel(d: DimensionNode) {
  return `${d.name} (${d.questions.length})`;
}

// 维度名称弹窗：新增与重命名复用同一弹窗（新增落库、之后也可改名）。
const dimensionModalOpen = ref(false);
const dimensionModalName = ref('');
const dimensionModalMode = ref<'create' | 'edit'>('create');
const dimensionModalTarget = ref<DimensionNode | null>(null);

function openCreateDimension() {
  dimensionModalMode.value = 'create';
  dimensionModalName.value = '';
  dimensionModalTarget.value = null;
  dimensionModalOpen.value = true;
}

function openRenameDimension(d: DimensionNode) {
  dimensionModalMode.value = 'edit';
  dimensionModalName.value = d.name;
  dimensionModalTarget.value = d;
  dimensionModalOpen.value = true;
}

async function submitDimensionModal() {
  const name = dimensionModalName.value.trim();
  if (!name) {
    message.warning(
      $t('plugin.linapro-eval-360.page.questionnaire.dimensionNameRequired'),
    );
    return;
  }
  if (dimensionModalMode.value === 'create') {
    const res = await dimensionCreate({
      questionnaireId: props.questionnaire.id,
      name,
      sort: props.questionnaire.dimensions.length,
    });
    message.success($t('plugin.linapro-eval-360.page.common.createSuccess'));
    selectedDimensionId.value = res.id;
  } else if (dimensionModalTarget.value) {
    await dimensionUpdate(dimensionModalTarget.value.id, { name });
    message.success($t('plugin.linapro-eval-360.page.common.updateSuccess'));
  }
  dimensionModalOpen.value = false;
  emit('changed');
}

async function deleteDimension(d: DimensionNode) {
  if (d.questions.length > 0) {
    message.warning(
      $t('plugin.linapro-eval-360.page.questionnaire.dimensionNotEmpty'),
    );
    return;
  }
  Modal.confirm({
    title: $t('plugin.linapro-eval-360.page.common.deleteConfirmTitle'),
    onOk: async () => {
      await dimensionDelete(d.id);
      message.success($t('plugin.linapro-eval-360.page.common.deleteSuccess'));
      selectedDimensionId.value = null;
      emit('changed');
    },
  });
}

// saveQuestion 保存题目。新增且指定了插入位置时（在某题之后插入），
// 先落库拿到新 id，再按目标顺序整体重排该维度题目，实现「后插入的题目占据该位置、原题整体顺延」。
// 注意：题目一旦创建成功，后续重排再失败也不能向上抛错——否则子组件会保留草稿卡，
// 用户重试将重复建题。此时降级为「已创建但排在末尾」并提示，让父组件正常刷新。
async function saveQuestion(
  input: QuestionSaveInput,
  id?: number,
  insertIndex?: number,
) {
  if (id && id > 0) {
    await questionUpdate(id, input);
  } else {
    const res = await questionCreate(input);
    if (insertIndex !== undefined && res?.id) {
      try {
        // 以落库前的题目顺序为基准，把新题插到目标位置，其余顺延。
        const dim = props.questionnaire.dimensions.find(
          (d) => d.id === input.dimensionId,
        );
        const ids = (dim?.questions ?? []).map((q) => q.id);
        ids.splice(Math.min(insertIndex, ids.length), 0, res.id);
        await questionReorder(props.questionnaire.id, input.dimensionId, ids);
      } catch {
        message.warning(
          $t('plugin.linapro-eval-360.page.questionnaire.reorderFailed'),
        );
      }
    }
  }
  message.success($t('plugin.linapro-eval-360.page.common.operateSuccess'));
  emit('changed');
}

async function deleteQuestion(id: number) {
  Modal.confirm({
    title: $t('plugin.linapro-eval-360.page.common.deleteConfirmTitle'),
    okType: 'danger',
    onOk: async () => {
      await questionDelete(id);
      message.success($t('plugin.linapro-eval-360.page.common.deleteSuccess'));
      emit('changed');
    },
  });
}
</script>

<template>
  <div class="flex gap-3" data-testid="eval360-questionnaire-editor">
    <!-- 左侧维度树 -->
    <div class="w-[240px] shrink-0">
      <div class="mb-2 flex items-center justify-between">
        <span class="text-sm font-semibold">
          {{ $t('plugin.linapro-eval-360.page.questionnaire.dimensions') }}
        </span>
        <a-button size="small" type="link" data-testid="eval360-dimension-add" @click="openCreateDimension">
          {{ $t('plugin.linapro-eval-360.page.common.add') }}
        </a-button>
      </div>
      <div class="flex flex-col gap-1">
        <div
          v-for="d in questionnaire.dimensions"
          :key="d.id"
          class="group flex cursor-pointer items-center justify-between rounded px-2 py-1"
          :class="{
            'bg-primary/10': selectedDimensionId === d.id,
            'hover:bg-gray-100': selectedDimensionId !== d.id,
          }"
          @click="selectedDimensionId = d.id"
        >
          <span class="truncate text-sm">{{ dimensionLabel(d) }}</span>
          <span class="hidden group-hover:flex">
            <a-button type="link" size="small" @click.stop="openRenameDimension(d)">
              {{ $t('plugin.linapro-eval-360.page.common.rename') }}
            </a-button>
            <a-button type="link" size="small" @click.stop="deleteDimension(d)">
              {{ $t('plugin.linapro-eval-360.page.common.delete') }}
            </a-button>
          </span>
        </div>
      </div>
    </div>
    <!-- 右侧题目列表 -->
    <div class="flex-1">
      <template v-if="selectedDimensionId">
        <QuestionEditor
          ref="questionEditorRef"
          :questionnaire="questionnaire"
          :dimension-id="selectedDimensionId"
          :evaluatees="evaluatees"
          :save-handler="saveQuestion"
          @delete="deleteQuestion"
        />
      </template>
      <Empty v-else :description="$t('plugin.linapro-eval-360.page.questionnaire.selectDimension')" />
    </div>

    <!-- 维度名称弹窗：新增与重命名复用 -->
    <Modal
      v-model:open="dimensionModalOpen"
      :title="
        dimensionModalMode === 'create'
          ? $t('plugin.linapro-eval-360.page.questionnaire.createDimension')
          : $t('plugin.linapro-eval-360.page.questionnaire.editDimension')
      "
      destroy-on-close
      @ok="submitDimensionModal"
    >
      <Input
        v-model:value="dimensionModalName"
        data-testid="eval360-dimension-name"
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.dimensionNamePlaceholder')"
        :maxlength="100"
        @press-enter="submitDimensionModal"
      />
    </Modal>
  </div>
</template>
