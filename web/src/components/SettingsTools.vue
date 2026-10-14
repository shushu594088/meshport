<script setup lang="ts">
import { ref } from "vue";
import { api, type State } from "../api";
import { preferences } from "../composables/usePreferences";
import Dialog from "./Dialog.vue";
import SecretField from "./SecretField.vue";
import SelectField from "./SelectField.vue";
import { useForm } from "../composables/useForm";
const props = defineProps<{
  state: State;
  disabled: boolean;
  dirty: boolean;
  execute: (task: () => Promise<unknown>, text: string) => Promise<boolean>;
}>();
const emit = defineEmits<{ restored: [] }>();
const modal = ref(""),
  error = ref(""),
  password = ref(""),
  file = ref<File | null>(null),
  config = ref(""),
  core = ref("");
const form = useForm();
const pageOptions = [
  { value: "overview", label: "概览" },
  { value: "subscriptions", label: "订阅" },
  { value: "nodes", label: "节点" },
  { value: "tests", label: "测试" },
  { value: "connections", label: "连接" },
  { value: "logs", label: "日志" },
  { value: "settings", label: "设置" },
];
function download(value: string, name: string, type = "application/json") {
  const url = URL.createObjectURL(new Blob([value], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function open(value: string) {
  password.value = "";
  error.value = "";
  file.value = null;
  modal.value = value;
}
async function run(task: () => Promise<unknown>, text: string) {
  error.value = "";
  return props.execute(async () => {
    try {
      return await task();
    } catch (e) {
      error.value = e instanceof Error ? e.message : "操作失败，请重试";
      throw e;
    }
  }, text);
}
async function showConfig() {
  if (
    await run(async () => {
      config.value = (await api<{ yaml: string }>("/config")).yaml;
    }, "当前配置已读取")
  )
    modal.value = "config";
}
async function coreInfo() {
  await run(async () => {
    const value = await api<{ version: string; meta?: boolean }>("/core");
    core.value = (value.meta ? "Mihomo " : "") + value.version;
  }, "内核信息已读取");
}
async function action(kind: string) {
  if (
    await run(
      () => api("/core/action", "POST", { action: kind }),
      kind === "geodata" ? "GeoData 已更新" : "DNS 缓存已清空",
    )
  )
    modal.value = "";
}
async function exportDiagnostics() {
  await run(
    async () =>
      download(
        JSON.stringify(await api("/diagnostics"), null, 2),
        "meshport-diagnostics.json",
      ),
    "脱敏诊断信息已导出",
  );
}
async function environment() {
  if (
    await run(async () => {
      const value = await api<{ value: string }>(
        "/environment?format=" + encodeURIComponent(preferences.environment),
      );
      try {
        await navigator.clipboard.writeText(value.value);
      } catch {
        download(
          value.value,
          "meshport-proxy." +
            (preferences.environment === "powershell"
              ? "ps1"
              : preferences.environment === "cmd"
                ? "cmd"
                : "sh"),
          "text/plain",
        );
      }
    }, "代理环境变量已复制或下载；仅供在这台服务器上使用")
  )
    modal.value = "";
}
async function backup(restore: boolean) {
  if (
    !(await form.validate(
      { backupPassword: password.value },
      {
        backupPassword: (v) => {
          const n = new TextEncoder().encode(String(v)).length;
          return n >= 12 && n <= 256 ? "" : "请输入 12–256 字节的备份密码";
        },
      },
    ))
  )
    return;
  if (restore && !file.value) {
    error.value = "请选择加密备份文件";
    return;
  }
  const ok = await run(
    async () => {
      if (restore) {
        if (file.value!.size > 9 * 1024 * 1024)
          throw new Error("备份文件超过 9 MiB 限制");
        let value;
        try {
          value = JSON.parse(await file.value!.text());
        } catch {
          throw new Error("备份文件格式无效");
        }
        await api("/backup/restore", "POST", {
          password: password.value,
          backup: value,
          revision: props.state.revision,
        });
        emit("restored");
      } else {
        download(
          JSON.stringify(
            await api("/backup", "POST", { password: password.value }),
          ),
          "meshport-backup-" + new Date().toISOString().slice(0, 10) + ".json",
        );
      }
    },
    restore
      ? "备份已恢复，TUN 已关闭，管理端口和管理员密码已保留"
      : "加密备份已下载，请妥善保存备份密码",
  );
  if (ok) {
    password.value = "";
    file.value = null;
    modal.value = "";
  }
}
</script>
<template>
  <section class="panel settings-tools">
    <h2>界面与工具</h2>
    <p>界面偏好立即保存到当前浏览器，服务器设置仍需点击“保存设置”。</p>
    <div class="field-grid">
      <SelectField
        id="themePreference"
        v-model="preferences.theme"
        label="主题模式"
        :options="[
          { value: 'light', label: '浅色' },
          { value: 'dark', label: '深色' },
          { value: 'system', label: '跟随系统' },
        ]"
      /><SelectField
        id="startPreference"
        v-model="preferences.startPage"
        label="启动页面"
        :options="pageOptions"
      />
    </div>
    <SelectField
      id="environmentPreference"
      v-model="preferences.environment"
      label="代理环境变量格式"
      :options="[
        { value: 'bash', label: 'Bash' },
        { value: 'powershell', label: 'PowerShell' },
        { value: 'cmd', label: 'CMD' },
      ]"
    />
    <div class="test-actions">
      <button
        class="outline"
        :disabled="disabled"
        @click="state.settings.allow_lan ? open('environment') : environment()"
      >
        复制代理环境变量</button
      ><button
        class="outline"
        :disabled="disabled || !state.active"
        @click="showConfig"
      >
        查看当前配置</button
      ><button class="outline" :disabled="disabled" @click="exportDiagnostics">
        导出诊断信息
      </button>
    </div>
    <p>
      环境变量使用
      127.0.0.1，适用于这台服务器的终端。当前配置与诊断信息均隐藏凭证。
    </p>
    <h3>内核维护</h3>
    <p>
      {{ core || "点击下方按钮读取实际内核版本。" }} · 外部控制仅监听服务器本机
    </p>
    <div class="test-actions">
      <button
        class="outline"
        :disabled="disabled || !state.service.running"
        @click="coreInfo"
      >
        读取内核版本</button
      ><button
        class="outline"
        :disabled="disabled || !state.service.running || !!state.pending_tun"
        @click="action('dns-flush')"
      >
        清空 DNS 缓存</button
      ><button
        class="outline"
        :disabled="disabled || !state.service.running || !!state.pending_tun"
        @click="open('geodata')"
      >
        更新 GeoData
      </button>
    </div>
    <h3>配置备份</h3>
    <p>
      备份包含订阅与代理凭证，使用独立密码加密。恢复会覆盖当前订阅和代理设置，保留管理员密码与
      Web 端口，并关闭 TUN。
    </p>
    <div class="test-actions">
      <button
        class="outline"
        :disabled="disabled || !!state.pending_tun"
        @click="open('backup')"
      >
        下载加密备份</button
      ><button
        class="outline"
        :disabled="disabled || dirty || !!state.pending_tun"
        @click="open('restore')"
      >
        恢复加密备份
      </button>
    </div>
    <p v-if="dirty">请先保存或放弃设置修改，再恢复备份。</p>
  </section>
  <Dialog
    :open="!!modal"
    :busy="disabled"
    :title="
      modal === 'config'
        ? '当前配置（已脱敏）'
        : modal === 'backup'
          ? '下载加密备份'
          : modal === 'restore'
            ? '恢复加密备份'
            : modal === 'geodata'
              ? '更新 GeoData？'
              : '复制含凭证的环境变量？'
    "
    @close="
      modal = '';
      password = '';
    "
  >
    <pre v-if="modal === 'config'" class="config-preview" tabindex="0">{{
      config
    }}</pre>
    <form
      v-else-if="modal === 'backup' || modal === 'restore'"
      novalidate
      @submit.prevent="backup(modal === 'restore')"
    >
      <p>
        {{
          modal === "restore"
            ? "将覆盖当前订阅和代理设置；TUN 会关闭，管理员密码和 Web 端口保留。运行中的连接可能中断。"
            : "请使用独立备份密码，密码丢失后无法恢复此备份。"
        }}
      </p>
      <SecretField
        id="backupPassword"
        v-model="password"
        label="备份密码"
        :error="form.errors.backupPassword"
        :disabled="disabled"
        autocomplete="off"
      />
      <div v-if="modal === 'restore'" class="field">
        <label for="backupFile">加密备份文件</label
        ><input
          id="backupFile"
          type="file"
          accept=".json,application/json"
          :disabled="disabled"
          @change="
            file = ($event.target as HTMLInputElement).files?.[0] || null
          "
        />
      </div>
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div class="dialog-actions">
        <button
          type="button"
          class="outline"
          data-safe-focus
          :disabled="disabled"
          @click="
            modal = '';
            password = '';
          "
        >
          取消</button
        ><button
          type="submit"
          :class="modal === 'restore' ? 'danger-button' : 'primary'"
          :disabled="disabled"
        >
          {{ modal === "restore" ? "覆盖并恢复" : "下载备份" }}
        </button>
      </div>
    </form>
    <template v-else-if="modal !== 'config'">
      <p>
        {{
          modal === "geodata"
            ? "内核将下载并更新 GeoData。网络失败时请重试，可能耗时约一分钟。"
            : "内容包含已保存的代理用户名与密码。请仅粘贴到可信的服务器终端；浏览器无法复制时会下载脚本文件。"
        }}
      </p>
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div class="dialog-actions">
        <button
          class="outline"
          data-safe-focus
          :disabled="disabled"
          @click="modal = ''"
        >
          取消</button
        ><button
          class="primary"
          :disabled="disabled"
          @click="modal === 'geodata' ? action('geodata') : environment()"
        >
          {{ modal === "geodata" ? "开始更新" : "复制或下载" }}
        </button>
      </div>
    </template>
  </Dialog>
</template>
