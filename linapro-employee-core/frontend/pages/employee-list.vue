<script lang="ts">
// 后台「通讯录管理 → 员工列表」页面。plugin.yaml 菜单 path=/contacts/employees 指向本页。
// pluginPageMeta.routePath 必须与菜单 path 完全一致，由 page-registry 匹配渲染。
export const pluginPageMeta = {
  routePath: "/contacts/employees",
  title: "员工列表",
};
</script>

<script setup lang="ts">
import type { EmployeeItem } from "./employee-client";

import { onMounted, ref } from "vue";

import { useAccess } from "@vben/access";
import { useVbenDrawer } from "@vben/common-ui";

import { Space, Tag } from "ant-design-vue";

import { platformTenantList } from "#/api/platform/tenant";
import { useVbenVxeGrid } from "#/adapter/vxe-table";
import { Page } from "#/plugins/dynamic";
import { formatTimestamp } from "#/utils/time";

import { listEmployees } from "./employee-client";
import EmployeeDetailDrawer from "./components/employee-detail-drawer.vue";

const accessCodes = {
  query: "linapro-employee-core:employee:query",
} as const;

const { hasAccessByCodes } = useAccess();
const tenantOptions = ref<{ label: string; value: number }[]>([]);

const [DetailDrawer, detailDrawerApi] = useVbenDrawer({
  connectedComponent: EmployeeDetailDrawer,
});

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    schema: [
      {
        component: "Input",
        fieldName: "keyword",
        label: "姓名/工号",
        componentProps: { placeholder: "姓名或工号" },
      },
      {
        component: "Select",
        fieldName: "tenantId",
        label: "所属租户",
        componentProps: () => ({ options: tenantOptions.value }),
      },
      {
        component: "Select",
        fieldName: "status",
        label: "在职状态",
        componentProps: {
          options: [
            { label: "在职", value: 1 },
            { label: "离职", value: 0 },
          ],
        },
      },
      {
        component: "Select",
        fieldName: "larkBound",
        label: "飞书绑定",
        componentProps: {
          options: [
            { label: "已绑定", value: true },
            { label: "未绑定", value: false },
          ],
        },
      },
    ],
    commonConfig: {
      labelWidth: 80,
      componentProps: { allowClear: true },
    },
    wrapperClass: "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
  },
  gridOptions: {
    columns: [
      { field: "empId", title: "EmpID", width: 100 },
      { field: "name", minWidth: 140, title: "姓名" },
      {
        field: "mokaEmployeeNo",
        formatter: ({ cellValue }) => cellValue || "—",
        minWidth: 140,
        title: "工号",
      },
      {
        field: "tenantName",
        formatter: ({ cellValue, row }) => cellValue || `#${row.tenantId}`,
        minWidth: 140,
        title: "所属租户",
      },
      {
        field: "status",
        slots: { default: "status" },
        title: "在职状态",
        width: 110,
      },
      {
        field: "larkBound",
        slots: { default: "larkBound" },
        title: "飞书绑定",
        width: 140,
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
        width: 100,
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
          return await listEmployees({
            pageNum: page.currentPage,
            pageSize: page.pageSize,
            ...formValues,
          });
        },
      },
    },
    rowConfig: { keyField: "empId" },
    id: "linapro-employee-core-employee-grid",
  },
});

onMounted(async () => {
  // 租户下拉是附加筛选：取不到时留空，不影响列表本身。
  try {
    const { items } = await platformTenantList({ pageNum: 1, pageSize: 100 });
    tenantOptions.value = items.map((tenant) => ({
      label: tenant.name,
      value: tenant.id,
    }));
  } catch {
    tenantOptions.value = [];
  }
});

function can(code: string) {
  return hasAccessByCodes([code]);
}

function handleDetail(row: EmployeeItem) {
  detailDrawerApi.setData({ empId: row.empId });
  detailDrawerApi.open();
}

function handleReload() {
  gridApi.query();
}
</script>

<template>
  <Page :auto-content-height="true" data-testid="employee-list-page">
    <!-- Grid 必须是 Page 的直接子节点：Page 内容区是定高 + overflow 容器，
         多套一层无高度的 div 会让 Grid 的 h-full 退化为内容高度，
         表格撑开、分页条被推到可视区之外（外层滚动条顶到底部翻不动）。 -->
    <Grid
      data-testid="linapro-employee-core-employee-grid"
      table-title="员工列表"
    >
      <template #status="{ row }">
        <Tag :color="row.status === 1 ? 'green' : 'default'">
          {{ row.status === 1 ? "在职" : "离职" }}
        </Tag>
      </template>

      <template #larkBound="{ row }">
        <template v-if="row.larkBound">
          <Tag color="blue">已绑定 {{ row.larkAppIds.length }} 个应用</Tag>
          <span class="ml-1 text-xs text-gray-500">
            {{ row.larkAppIds.join(", ") }}
          </span>
        </template>
        <Tag v-else>未绑定</Tag>
      </template>

      <template #action="{ row }">
        <Space>
          <ghost-button
            v-if="can(accessCodes.query)"
            :data-testid="`employee-detail-${row.empId}`"
            @click.stop="handleDetail(row)"
          >
            详情
          </ghost-button>
        </Space>
      </template>
    </Grid>
    <DetailDrawer @reload="handleReload" />
  </Page>
</template>
