// data.ts 收拢后台「通讯录管理 → 员工列表」的查询表单 schema 与表格列定义。
import type { VbenFormSchema } from "#/adapter/form";
import type { VxeGridProps } from "#/adapter/vxe-table";

import { formatTimestamp } from "#/utils/time";

/** 查询表单 schema。所属租户下拉的选项由页面挂载后异步取回再 updateSchema 注入。 */
export function buildQuerySchema(): VbenFormSchema[] {
  return [
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
      componentProps: {
        options: [] as { label: string; value: number }[],
      },
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
  ];
}

/** 表格列定义。在职状态、飞书绑定、操作三列走页面模板插槽。 */
export function buildColumns(): VxeGridProps["columns"] {
  return [
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
    // 三个任职日期来自 Moka 同步、后台只读；空值（Moka 未提供）显示占位符。
    {
      field: "onBoardingDate",
      formatter: ({ cellValue }) => cellValue || "—",
      title: "入职时间",
      width: 120,
    },
    {
      field: "endProbationDate",
      formatter: ({ cellValue }) => cellValue || "—",
      title: "转正时间",
      width: 120,
    },
    {
      field: "leaveDate",
      formatter: ({ cellValue }) => cellValue || "—",
      title: "离职时间",
      width: 120,
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
  ];
}
