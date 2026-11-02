<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";

import { useVbenModal } from "@vben/common-ui";

import {
  Alert,
  Button,
  Form,
  FormItem,
  Input,
  RadioGroup,
  Select,
  message,
} from "ant-design-vue";

import {
  createApiKey,
  getApiKey,
  type PushGroup,
  updateApiKey,
} from "../api-key-client";

const props = defineProps<{
  groups: PushGroup[];
  statusDicts: any[];
}>();

const emit = defineEmits<{
  created: [payload: { appKey: string; secret: string }];
  reload: [];
}>();

interface FormData {
  id?: number;
  name: string;
  appKey: string;
  secret: string;
  group: string | undefined;
  chatId: string;
  chatPushAppId: string | undefined;
  status: number;
  remark: string;
}

const defaultValues: FormData = {
  id: undefined,
  name: "",
  appKey: "",
  secret: "",
  group: undefined,
  chatId: "",
  chatPushAppId: undefined,
  status: 1,
  remark: "",
};

const formData = ref<FormData>({ ...defaultValues });
const isEdit = computed(() => !!formData.value.id);
const modalTitle = computed(() => (isEdit.value ? "编辑凭据" : "新增凭据"));
const groupOptions = computed(() =>
  props.groups.map((group) => ({ label: group.name, value: group.name })),
);
/** 当前所选分组内的推送应用，供群聊选择发送应用。 */
const pushAppOptions = computed(() =>
  (
    props.groups.find((group) => group.name === formData.value.group)
      ?.pushApps ?? []
  ).map((app) => ({
    label: app.name ? `${app.name}（${app.appId}）` : app.appId,
    value: app.appId,
  })),
);
/** 配置了群聊且分组有多个推送应用时，必须指定发送群聊的推送应用。 */
const chatPushAppRequired = computed(
  () => !!formData.value.chatId.trim() && pushAppOptions.value.length > 1,
);

// 切换分组后，原选的推送应用若不属于新分组则清空，避免提交跨分组的应用。
watch(
  () => formData.value.group,
  () => {
    const current = formData.value.chatPushAppId;
    if (
      current &&
      !pushAppOptions.value.some((item) => item.value === current)
    ) {
      formData.value.chatPushAppId = undefined;
    }
  },
);

const statusOptions = computed(() =>
  props.statusDicts.map((item: any) => ({
    label: item.label,
    value: Number(item.value),
  })),
);

const formRules = reactive({
  name: [{ message: "请输入名称", required: true }],
  appKey: [
    { message: "请输入 app_key", required: true },
    {
      message: "仅允许 1~64 个字母、数字、_、-、. 字符",
      pattern: /^[\w.-]{1,64}$/,
    },
  ],
  group: [{ message: "请选择推送分组", required: true }],
  chatId: [
    {
      message: "chat_id 须以 oc_ 开头，仅含字母与数字",
      pattern: /^$|^oc_[A-Za-z0-9]{1,120}$/,
    },
  ],
  chatPushAppId: [
    {
      validator: async (_rule: unknown, value: string | undefined) => {
        if (chatPushAppRequired.value && !value) {
          throw new Error("该分组有多个推送应用，请选择发送群聊的推送应用");
        }
      },
    },
  ],
  secret: [
    {
      // 新增必填（服务端不再代为生成）；编辑时留空表示保持原值。
      validator: async (_rule: unknown, value: string) => {
        if (!isEdit.value && !value.trim()) {
          throw new Error("请输入 secret，或点击「生成」");
        }
      },
    },
    {
      message: "secret 须为 16~128 个可见 ASCII 字符",
      pattern: /^$|^[\x21-\x7E]{16,128}$/,
    },
  ],
});

const { resetFields, validate, validateInfos } = Form.useForm(
  formData,
  formRules,
);

const [Modal, modalApi] = useVbenModal({
  class: "w-[600px] max-w-[calc(100vw-32px)]",
  onConfirm: handleConfirm,
  onOpenChange: async (isOpen: boolean) => {
    if (!isOpen) return;
    const data = modalApi.getData<{ id?: number }>();
    formData.value = { ...defaultValues };
    resetFields();
    if (!data?.id) {
      // 新增时预填随机 app_key 与 secret，仍允许手动修改。
      formData.value.appKey = generateAppKey();
      handleGenerateSecret();
      return;
    }

    modalApi.setState({ confirmLoading: true });
    try {
      const item = await getApiKey(data.id);
      formData.value = {
        id: item.id,
        name: item.name,
        appKey: item.appKey,
        secret: "",
        group: item.group,
        chatId: item.chatId || "",
        chatPushAppId: item.chatPushAppId || undefined,
        status: item.status,
        remark: item.remark || "",
      };
    } finally {
      modalApi.setState({ confirmLoading: false });
    }
  },
});

const LOWER_CHARS = "abcdefghijklmnopqrstuvwxyz";
const UPPER_CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
const DIGIT_CHARS = "0123456789";

/** 用密码学随机数从字符集中取指定长度的字符串；拒绝采样避免取模偏差。 */
function randomString(length: number, charset: string): string {
  const limit = 256 - (256 % charset.length);
  const buf = new Uint8Array(1);
  let result = "";
  while (result.length < length) {
    crypto.getRandomValues(buf);
    const byte = buf[0]!;
    if (byte < limit) result += charset[byte % charset.length];
  }
  return result;
}

/** 生成 16 位小写字母+数字的 app_key，新增凭据时自动填入。 */
function generateAppKey(): string {
  return randomString(16, LOWER_CHARS + DIGIT_CHARS);
}

/** 在浏览器本地生成 64 位大小写字母+数字的 secret（保证大写、小写、数字各至少一个）；新增时 secret 必填，服务端不代为生成。 */
function handleGenerateSecret() {
  let secret = "";
  do {
    secret = randomString(64, LOWER_CHARS + UPPER_CHARS + DIGIT_CHARS);
  } while (
    !/[a-z]/.test(secret) ||
    !/[A-Z]/.test(secret) ||
    !/\d/.test(secret)
  );
  formData.value.secret = secret;
}

async function handleConfirm() {
  try {
    modalApi.lock(true);
    await validate();
    const chatId = formData.value.chatId.trim();
    const payload = {
      name: formData.value.name.trim(),
      appKey: formData.value.appKey.trim(),
      secret: formData.value.secret.trim(),
      group: formData.value.group ?? "",
      chatId,
      // 未配置群聊时不提交推送应用（后端拒绝「无群聊却指定推送应用」）。
      chatPushAppId: chatId ? (formData.value.chatPushAppId ?? "") : "",
      status: formData.value.status,
      remark: formData.value.remark.trim(),
    };
    if (isEdit.value && formData.value.id) {
      await updateApiKey(formData.value.id, payload);
      message.success("保存成功");
    } else {
      const res = await createApiKey(payload);
      message.success("新增成功");
      emit("created", { appKey: payload.appKey, secret: res.secret });
    }
    emit("reload");
    modalApi.close();
  } finally {
    modalApi.lock(false);
  }
}
</script>

<template>
  <Modal :title="modalTitle">
    <Form layout="vertical" data-testid="lark-push-api-key-form">
      <FormItem label="名称" v-bind="validateInfos.name">
        <Input
          v-model:value="formData.name"
          data-testid="lark-push-api-key-name"
          :maxlength="64"
          placeholder="调用方名称，如：运维告警平台"
        />
      </FormItem>
      <FormItem label="app_key" v-bind="validateInfos.appKey">
        <Input
          v-model:value="formData.appKey"
          data-testid="lark-push-api-key-app-key"
          :maxlength="64"
          placeholder="公开标识，调用方放在 X-App-Key 请求头"
        />
      </FormItem>
      <FormItem label="推送分组" v-bind="validateInfos.group">
        <Select
          v-model:value="formData.group"
          data-testid="lark-push-api-key-group"
          :options="groupOptions"
          placeholder="选择 push_groups 中已配置的分组"
        />
      </FormItem>
      <FormItem label="群聊 chat_id" v-bind="validateInfos.chatId">
        <Input
          v-model:value="formData.chatId"
          allow-clear
          data-testid="lark-push-api-key-chat-id"
          :maxlength="128"
          placeholder="可选，oc_ 开头；配置后该凭据的每次推送都会发到这个群"
        />
      </FormItem>
      <FormItem
        v-if="formData.chatId.trim()"
        label="群聊推送应用"
        :required="chatPushAppRequired"
        v-bind="validateInfos.chatPushAppId"
      >
        <Select
          v-model:value="formData.chatPushAppId"
          allow-clear
          data-testid="lark-push-api-key-chat-push-app"
          :options="pushAppOptions"
          :placeholder="
            chatPushAppRequired
              ? '分组有多个推送应用，请选择由哪个机器人发到群里'
              : '可不选，默认使用分组唯一的推送应用'
          "
        />
      </FormItem>
      <Alert
        v-if="formData.chatId.trim()"
        class="mb-4"
        message="所选推送应用的机器人必须已加入该群，否则群聊明细会返回失败。"
        show-icon
        type="info"
      />
      <FormItem
        label="secret"
        :required="!isEdit"
        v-bind="validateInfos.secret"
      >
        <div class="flex w-full gap-2">
          <Input
            v-model:value="formData.secret"
            data-testid="lark-push-api-key-secret"
            :maxlength="128"
            :placeholder="isEdit ? '留空则保持不变' : '必填，可点击「生成」'"
          />
          <Button
            data-testid="lark-push-api-key-generate"
            @click="handleGenerateSecret"
          >
            生成
          </Button>
        </div>
      </FormItem>
      <Alert
        v-if="isEdit"
        class="mb-4"
        message="填写新的 secret 会立即替换旧值，调用方须同步更换，否则签名请求将被拒绝。"
        show-icon
        type="warning"
      />
      <FormItem label="状态">
        <RadioGroup
          v-model:value="formData.status"
          button-style="solid"
          data-testid="lark-push-api-key-status"
          option-type="button"
          :options="statusOptions"
        />
      </FormItem>
      <FormItem label="备注">
        <Input.TextArea
          v-model:value="formData.remark"
          data-testid="lark-push-api-key-remark"
          :maxlength="500"
          :rows="3"
          show-count
        />
      </FormItem>
    </Form>
  </Modal>
</template>
