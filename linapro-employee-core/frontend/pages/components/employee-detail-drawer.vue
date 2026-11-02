<script setup lang="ts">
// 员工详情抽屉：基本信息（同步维护，只读）+ 关联后台账号 + 部门 + 全部飞书身份。
//
// 可编辑的只有同步不写的两类字段：飞书身份绑定 / 解绑、关联后台账号。
// 姓名、工号、租户、在职状态由 Moka 全量同步维护，这里只读（见 design D25）。
import type {
  EmployeeDetail,
  LarkAppOption,
  LarkIdentityItem,
  SysUserOption,
} from "../employee-client";

import { computed, ref } from "vue";

import { useVbenDrawer } from "@vben/common-ui";
import { useAccess } from "@vben/access";

import {
  Button,
  Descriptions,
  DescriptionsItem,
  Input,
  message,
  Popconfirm,
  Select,
  Space,
  Spin,
  Table,
  Tag,
} from "ant-design-vue";

import { formatTimestamp } from "#/utils/time";

import {
  bindLarkIdentity,
  getEmployee,
  listLarkApps,
  listSysUserOptions,
  setEmployeeSysUser,
  unbindLarkIdentity,
} from "../employee-client";

const emit = defineEmits<{ reload: [] }>();

const accessCodes = {
  query: "linapro-employee-core:employee:query",
  edit: "linapro-employee-core:employee:edit",
} as const;

const { hasAccessByCodes } = useAccess();

const detail = ref<EmployeeDetail | null>(null);
const loading = ref(false);

// 关联后台账号编辑态
const sysUserEditing = ref(false);
const sysUserOptions = ref<SysUserOption[]>([]);
const sysUserSearching = ref(false);
const pickedSysUser = ref<number | undefined>();

// 飞书身份绑定表单
const binding = ref(false);
const larkApps = ref<LarkAppOption[]>([]);
const bindAppID = ref<string | undefined>();
const bindOpenID = ref("");

const sysUserSelectOptions = computed(() =>
  sysUserOptions.value.map((u) => ({
    label: u.nickname ? `${u.nickname}（${u.username}）` : u.username,
    value: u.id,
  })),
);

const larkAppSelectOptions = computed(() =>
  larkApps.value.map((app) => ({
    label: app.name ? `${app.name}（${app.larkAppId}）` : app.larkAppId,
    value: app.larkAppId,
  })),
);

const identityColumns = [
  {
    title: "飞书应用",
    dataIndex: "larkAppId",
    customRender: ({ record }: { record: LarkIdentityItem }) =>
      record.larkAppName
        ? `${record.larkAppName}（${record.larkAppId}）`
        : record.larkAppId,
  },
  { title: "open_id", dataIndex: "openId", width: 220 },
  { title: "user_id", dataIndex: "userId", width: 200 },
  { title: "来源", dataIndex: "idSource", width: 200 },
  { title: "绑定时间", dataIndex: "createdAt", width: 180 },
  { title: "操作", dataIndex: "action", width: 100 },
];

const [Drawer, drawerApi] = useVbenDrawer({
  onOpenChange: async (open: boolean) => {
    if (!open) {
      reset();
      return;
    }
    const { empId } = drawerApi.getData() as { empId: number };
    await load(empId);
  },
});

async function load(empId: number) {
  loading.value = true;
  try {
    detail.value = await getEmployee(empId);
  } finally {
    loading.value = false;
  }
}

function reset() {
  detail.value = null;
  sysUserEditing.value = false;
  sysUserOptions.value = [];
  pickedSysUser.value = undefined;
  binding.value = false;
  bindAppID.value = undefined;
  bindOpenID.value = "";
}

function can(code: string) {
  return hasAccessByCodes([code]);
}

/** 改动成功后同时刷新抽屉与列表：列表的绑定摘要随身份变化。 */
async function refresh() {
  if (!detail.value) {
    return;
  }
  await load(detail.value.empId);
  emit("reload");
}

async function openSysUserEdit() {
  if (!detail.value) {
    return;
  }
  sysUserEditing.value = true;
  pickedSysUser.value = detail.value.sysUserId || undefined;
  await searchSysUsers("");
}

async function searchSysUsers(keyword: string) {
  sysUserSearching.value = true;
  try {
    sysUserOptions.value = await listSysUserOptions(keyword);
  } finally {
    sysUserSearching.value = false;
  }
}

async function saveSysUser() {
  if (!detail.value) {
    return;
  }
  await setEmployeeSysUser(detail.value.empId, pickedSysUser.value ?? 0);
  message.success("关联后台账号已更新");
  sysUserEditing.value = false;
  await refresh();
}

async function openBinding() {
  binding.value = true;
  bindOpenID.value = "";
  bindAppID.value = undefined;
  if (larkApps.value.length === 0) {
    larkApps.value = await listLarkApps();
  }
  bindAppID.value = larkApps.value[0]?.larkAppId;
}

async function submitBinding() {
  if (!detail.value || !bindAppID.value || !bindOpenID.value.trim()) {
    return;
  }
  await bindLarkIdentity(
    detail.value.empId,
    bindAppID.value,
    bindOpenID.value.trim(),
  );
  message.success("飞书身份已绑定");
  binding.value = false;
  await refresh();
}

async function handleUnbind(record: LarkIdentityItem) {
  if (!detail.value) {
    return;
  }
  await unbindLarkIdentity(detail.value.empId, record.larkAppId, record.openId);
  message.success("飞书身份已解绑");
  await refresh();
}
</script>

<template>
  <Drawer class="w-[860px] max-w-[calc(100vw-32px)]" title="员工详情">
    <Spin :spinning="loading">
      <template v-if="detail">
        <Descriptions
          :column="2"
          bordered
          size="middle"
          :label-style="{ minWidth: '110px' }"
          :content-style="{ minWidth: '120px' }"
        >
          <DescriptionsItem label="EmpID">{{ detail.empId }}</DescriptionsItem>
          <DescriptionsItem label="姓名">{{ detail.name }}</DescriptionsItem>
          <DescriptionsItem label="工号">
            {{ detail.mokaEmployeeNo || "—" }}
          </DescriptionsItem>
          <DescriptionsItem label="所属租户">
            {{ detail.tenantName || `#${detail.tenantId}` }}
          </DescriptionsItem>
          <DescriptionsItem label="在职状态">
            <Tag :color="detail.status === 1 ? 'green' : 'default'">
              {{ detail.status === 1 ? "在职" : "离职" }}
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem label="部门">
            {{
              detail.departmentIds.length > 0
                ? detail.departmentIds.join(", ")
                : "暂无"
            }}
          </DescriptionsItem>
          <DescriptionsItem label="创建时间">
            {{ formatTimestamp(detail.createdAt) }}
          </DescriptionsItem>
          <DescriptionsItem label="更新时间">
            {{ formatTimestamp(detail.updatedAt) }}
          </DescriptionsItem>
        </Descriptions>

        <p class="mt-2 text-xs text-gray-500">
          以上字段由 Moka 全量同步维护，后台只读。
        </p>

        <div class="mt-6">
          <div class="mb-2 flex items-center justify-between">
            <span class="font-medium">关联后台账号</span>
            <Button
              v-if="can(accessCodes.edit) && !sysUserEditing"
              size="small"
              @click="openSysUserEdit"
            >
              修改
            </Button>
          </div>
          <template v-if="sysUserEditing">
            <Space wrap>
              <Select
                v-model:value="pickedSysUser"
                :filter-option="false"
                :loading="sysUserSearching"
                :options="sysUserSelectOptions"
                allow-clear
                class="w-72"
                placeholder="搜索并选择后台账号"
                show-search
                @search="searchSysUsers"
              />
              <Button type="primary" @click="saveSysUser">保存</Button>
              <Button @click="sysUserEditing = false">取消</Button>
            </Space>
            <p class="mt-1 text-xs text-gray-500">
              一个后台账号最多关联一名员工。
            </p>
          </template>
          <template v-else>
            <span v-if="detail.sysUserId !== 0">
              {{
                detail.sysUserNickname ||
                detail.sysUserName ||
                `#${detail.sysUserId}`
              }}
              <span v-if="detail.sysUserName" class="text-gray-500">
                （{{ detail.sysUserName }}）
              </span>
            </span>
            <span v-else class="text-gray-500">未关联</span>
          </template>
        </div>

        <div class="mt-6">
          <div class="mb-2 flex items-center justify-between">
            <span class="font-medium">飞书身份</span>
            <Button
              v-if="can(accessCodes.edit) && !binding"
              size="small"
              @click="openBinding"
            >
              绑定飞书身份
            </Button>
          </div>

          <Space v-if="binding" class="mb-3" wrap>
            <Select
              v-model:value="bindAppID"
              :options="larkAppSelectOptions"
              class="w-64"
              placeholder="选择飞书应用"
            />
            <Input
              v-model:value="bindOpenID"
              class="w-72"
              placeholder="飞书 open_id（ou_xxx）"
            />
            <Button type="primary" @click="submitBinding">绑定</Button>
            <Button @click="binding = false">取消</Button>
          </Space>

          <p v-if="binding" class="mb-2 text-xs text-gray-500">
            绑定前会校验该 open_id
            在所选应用通讯录中存在；若该成员能按工号匹配，后续同步会以工号匹配结果为准。
          </p>

          <Table
            :columns="identityColumns"
            :data-source="detail.larkIdentities"
            :pagination="false"
            row-key="openId"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'idSource'">
                <Tag v-if="record.idSource === 'own'" color="blue">
                  本企业（可登录）
                </Tag>
                <Tag v-else>关联组织（不可登录）</Tag>
              </template>
              <template v-else-if="column.dataIndex === 'userId'">
                {{ record.userId || "—" }}
              </template>
              <template v-else-if="column.dataIndex === 'createdAt'">
                {{ formatTimestamp(record.createdAt) }}
              </template>
              <template v-else-if="column.dataIndex === 'action'">
                <Popconfirm
                  v-if="can(accessCodes.edit)"
                  title="解绑后该员工将无法用此身份登录，确认解绑？"
                  @confirm="handleUnbind(record)"
                >
                  <Button danger size="small" type="link">解绑</Button>
                </Popconfirm>
              </template>
            </template>
          </Table>
          <p
            v-if="detail.larkIdentities.length === 0"
            class="mt-2 text-xs text-gray-500"
          >
            暂无飞书身份：该员工无法登录员工端，也无法接收飞书推送。
          </p>
        </div>
      </template>
    </Spin>
  </Drawer>
</template>
