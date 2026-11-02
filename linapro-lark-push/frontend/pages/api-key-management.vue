<script lang="ts">
export const pluginPageMeta = {
  routePath: "linapro-lark-push-api-keys",
  title: "飞书推送凭据",
};
</script>

<script setup lang="ts">
import type { ApiKeyItem } from "./api-key-client";

import { onMounted, ref } from "vue";

import { useAccess } from "@vben/access";
import { useVbenModal } from "@vben/common-ui";

import { Popconfirm, Space, Switch } from "ant-design-vue";

import { useVbenVxeGrid } from "#/adapter/vxe-table";
import { DictTag } from "#/components/dict";
import { Page } from "#/plugins/dynamic";
import { useDictStore } from "#/store/dict";
import { formatTimestamp } from "#/utils/time";

import {
  deleteApiKey,
  listApiKeys,
  listPushGroups,
  type PushGroup,
  updateApiKeyStatus,
} from "./api-key-client";
import ApiKeyModal from "./components/api-key-modal.vue";
import ApiKeySecretModal from "./components/api-key-secret-modal.vue";

/** 状态字典：与后端 Status 常量一致（1=正常 0=停用）。 */
const STATUS_DICT = "sys_normal_disable";

const accessCodes = {
  add: "linapro-lark-push:api-key:add",
  edit: "linapro-lark-push:api-key:edit",
  remove: "linapro-lark-push:api-key:remove",
  secret: "linapro-lark-push:api-key:secret",
} as const;

const { hasAccessByCodes } = useAccess();
const dictStore = useDictStore();
const statusDicts = ref<any[]>([]);
const pushGroups = ref<PushGroup[]>([]);
const groupOptions = ref<{ label: string; value: string }[]>([]);
const statusChangingIds = ref(new Set<number>());

const [EditModal, editModalApi] = useVbenModal({
  connectedComponent: ApiKeyModal,
});
const [SecretModal, secretModalApi] = useVbenModal({
  connectedComponent: ApiKeySecretModal,
});

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    schema: [
      {
        component: "Input",
        fieldName: "keyword",
        label: "关键字",
        componentProps: { placeholder: "名称或 app_key" },
      },
      {
        component: "Select",
        fieldName: "group",
        label: "推送分组",
        componentProps: () => ({ options: groupOptions.value }),
      },
      {
        component: "Select",
        fieldName: "status",
        label: "状态",
        componentProps: () => ({
          options: statusDicts.value.map((item: any) => ({
            label: item.label,
            value: Number(item.value),
          })),
        }),
      },
    ],
    commonConfig: {
      labelWidth: 80,
      componentProps: { allowClear: true },
    },
    wrapperClass: "grid-cols-1 md:grid-cols-2 lg:grid-cols-3",
  },
  gridOptions: {
    columns: [
      { field: "name", minWidth: 160, title: "名称" },
      { field: "appKey", minWidth: 180, title: "app_key" },
      { field: "group", minWidth: 160, title: "推送分组" },
      {
        field: "chatId",
        formatter: ({ cellValue }) => cellValue || "—",
        minWidth: 200,
        showOverflow: "ellipsis",
        title: "群聊 chat_id",
      },
      {
        field: "status",
        slots: { default: "status" },
        title: "状态",
        width: 110,
      },
      {
        field: "remark",
        minWidth: 180,
        showOverflow: "ellipsis",
        title: "备注",
      },
      {
        field: "updatedAt",
        formatter: ({ cellValue }) => formatTimestamp(cellValue),
        title: "更新时间",
        width: 180,
      },
      {
        field: "action",
        fixed: "right",
        slots: { default: "action" },
        title: "操作",
        width: 220,
      },
    ],
    height: "auto",
    keepSource: true,
    pagerConfig: {},
    proxyConfig: {
      ajax: {
        query: async (
          { page }: { page: { currentPage: number; pageSize: number } },
          formValues = {},
        ) => {
          return await listApiKeys({
            pageNum: page.currentPage,
            pageSize: page.pageSize,
            ...formValues,
          });
        },
      },
    },
    rowConfig: { keyField: "id" },
    id: "linapro-lark-push-api-key-grid",
  },
});

onMounted(async () => {
  const [dicts, groups] = await Promise.all([
    dictStore.getDictOptionsAsync(STATUS_DICT),
    listPushGroups(),
  ]);
  statusDicts.value = dicts;
  pushGroups.value = groups;
  groupOptions.value = groups.map((group) => ({
    label: group.name,
    value: group.name,
  }));
});

function can(code: string) {
  return hasAccessByCodes([code]);
}

function handleAdd() {
  editModalApi.setData({});
  editModalApi.open();
}

function handleEdit(row: ApiKeyItem) {
  editModalApi.setData({ id: row.id });
  editModalApi.open();
}

function handleViewSecret(row: ApiKeyItem) {
  secretModalApi.setData({ id: row.id, appKey: row.appKey });
  secretModalApi.open();
}

/** 新增成功后直接展示保存的 secret，便于当场交给调用方。 */
function handleCreated(payload: { appKey: string; secret: string }) {
  secretModalApi.setData(payload);
  secretModalApi.open();
}

async function handleDelete(row: ApiKeyItem) {
  await deleteApiKey(row.id);
  await gridApi.query();
}

async function handleStatusChange(row: ApiKeyItem, checked: boolean) {
  const next = checked ? 1 : 0;
  if (row.status === next || statusChangingIds.value.has(row.id)) {
    return;
  }
  // 请求进行中保持开关为当前值，接口成功后才落到目标状态。
  statusChangingIds.value = new Set(statusChangingIds.value).add(row.id);
  try {
    await updateApiKeyStatus(row.id, next);
    row.status = next;
  } catch {
    await gridApi.query();
  } finally {
    const rest = new Set(statusChangingIds.value);
    rest.delete(row.id);
    statusChangingIds.value = rest;
  }
}

function handleReload() {
  gridApi.query();
}
</script>

<template>
  <Page :auto-content-height="true">
    <Grid table-title="飞书推送凭据">
      <template #toolbar-tools>
        <Space>
          <a-button
            v-if="can(accessCodes.add)"
            data-testid="lark-push-api-key-add"
            type="primary"
            @click="handleAdd"
          >
            新增
          </a-button>
        </Space>
      </template>

      <template #status="{ row }">
        <Switch
          v-if="can(accessCodes.edit)"
          :checked="row.status === 1"
          :data-testid="`lark-push-api-key-status-${row.id}`"
          :loading="statusChangingIds.has(row.id)"
          checked-children="正常"
          un-checked-children="停用"
          @change="(checked) => handleStatusChange(row, !!checked)"
        />
        <DictTag v-else :dicts="statusDicts" :value="String(row.status)" />
      </template>

      <template #action="{ row }">
        <Space>
          <ghost-button
            v-if="can(accessCodes.edit)"
            :data-testid="`lark-push-api-key-edit-${row.id}`"
            @click.stop="handleEdit(row)"
          >
            编辑
          </ghost-button>
          <ghost-button
            v-if="can(accessCodes.secret)"
            :data-testid="`lark-push-api-key-secret-${row.id}`"
            @click.stop="handleViewSecret(row)"
          >
            查看密钥
          </ghost-button>
          <Popconfirm
            v-if="can(accessCodes.remove)"
            title="删除后以该 app_key 签名的请求将立即被拒绝，确认删除？"
            @confirm="handleDelete(row)"
          >
            <ghost-button
              danger
              :data-testid="`lark-push-api-key-delete-${row.id}`"
              @click.stop=""
            >
              删除
            </ghost-button>
          </Popconfirm>
        </Space>
      </template>
    </Grid>
    <EditModal
      :groups="pushGroups"
      :status-dicts="statusDicts"
      @created="handleCreated"
      @reload="handleReload"
    />
    <SecretModal />
  </Page>
</template>
