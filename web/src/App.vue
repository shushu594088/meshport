<script setup lang="ts">
import {
  ref,
  computed,
  watch,
  onMounted,
  onBeforeUnmount,
  nextTick,
} from "vue";
import {
  Cable,
  LayoutDashboard,
  Layers,
  Network,
  Settings2,
  LogOut,
  Play,
  Square,
  RotateCw,
  Plus,
  RefreshCw,
  Trash2,
  Check,
  ArrowRight,
  ShieldCheck,
  Search,
  X,
  LoaderCircle,
  ExternalLink,
} from "lucide-vue-next";
import {
  api,
  APIError,
  type State,
  type Settings,
  type Proxy,
  type Subscription,
} from "./api";
import Field from "./components/Field.vue";
import SecretField from "./components/SecretField.vue";
import Dialog from "./components/Dialog.vue";
import ModeControl from "./components/ModeControl.vue";
import SelectField from "./components/SelectField.vue";
import { useForm, passwordRule } from "./composables/useForm";

const pages = [
  { id: "overview", name: "概览", icon: LayoutDashboard },
  { id: "subscriptions", name: "订阅", icon: Layers },
  { id: "nodes", name: "节点", icon: Network },
  { id: "settings", name: "设置", icon: Settings2 },
];
const route = ref("overview"),
  query = ref(""),
  page = ref(1),
  composing = ref(false);
const auth = ref<{ initialized: boolean; authenticated: boolean } | null>(null),
  state = ref<State | null>(null),
  draft = ref<Settings | null>(null),
  draftRevision = ref(0);
const busy = ref(false),
  loading = ref(true),
  error = ref(""),
  success = ref(""),
  dialog = ref(""),
  confirmAction = ref<(() => Promise<boolean | void>) | null>(null),
  confirmTitle = ref(""),
  confirmText = ref(""),
  confirmLabel = ref("确认");
const authPassword = ref(""),
  initToken = ref(""),
  newName = ref(""),
  newURL = ref(""),
  newContent = ref(""),
  importMode = ref("url"),
  currentPassword = ref(""),
  newPassword = ref(""),
  proxies = ref<Record<string, Proxy>>({}),
  nodeError = ref(""),
  group = ref(""),
  now = ref(Date.now()),
  bypassText = ref("");
const form = useForm();
const { errors } = form;
let refreshID = 0,
  refreshing = false,
  nodeID = 0,
  poll: ReturnType<typeof setInterval>,
  clock: ReturnType<typeof setInterval>;
const activeSub = computed(() =>
  state.value?.subscriptions.find((s) => s.id === state.value?.active),
);
const isDirty = computed(
  () =>
    draft.value &&
    state.value &&
    (JSON.stringify({
      ...draft.value,
      proxy_password: undefined,
      has_proxy_password: undefined,
    }) !==
      JSON.stringify({
        ...state.value.settings,
        proxy_password: undefined,
        has_proxy_password: undefined,
      }) ||
      !!draft.value.proxy_password ||
      bypassText.value !== state.value.settings.bypass.join("\n")),
);
const seconds = computed(() =>
  state.value?.pending_tun
    ? Math.max(
        0,
        Math.ceil(
          (new Date(state.value.pending_tun.deadline).getTime() - now.value) /
            1000,
        ),
      )
    : 0,
);
const title = computed(
  () => pages.find((p) => p.id === route.value)?.name || "概览",
);
const groups = computed(() =>
  Object.values(proxies.value).filter((p) => p.all?.length),
);
const currentGroup = computed(() => proxies.value[group.value]);
const records = computed(() =>
  route.value === "subscriptions"
    ? state.value?.subscriptions || []
    : (currentGroup.value?.all || []).map((name) => ({
        ...proxies.value[name],
        name,
      })),
);
const filtered = computed(() =>
  records.value.filter((s) =>
    s.name
      .toLocaleLowerCase("zh-CN")
      .includes(query.value.toLocaleLowerCase("zh-CN")),
  ),
);
const totalPages = computed(() =>
  Math.max(1, Math.ceil(filtered.value.length / 20)),
);
const visible = computed(() =>
  filtered.value.slice((page.value - 1) * 20, page.value * 20),
);
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
function date(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
function message(e: unknown) {
  return e instanceof Error ? e.message : "操作失败，请重试";
}
function copyDraft() {
  if (!state.value) return;
  Object.keys(errors).forEach((key) => delete errors[key]);
  draft.value = JSON.parse(JSON.stringify(state.value.settings));
  draftRevision.value = state.value.revision;
  draft.value!.proxy_password = "";
  bypassText.value = draft.value!.bypass.join("\n");
}
async function refresh(force = false) {
  if (refreshing && !force) return;
  refreshing = true;
  const id = ++refreshID;
  try {
    const value = await api<State>("/state");
    if (id !== refreshID) return;
    const dirty = isDirty.value;
    state.value = value;
    if (!draft.value || (!dirty && route.value !== "settings")) copyDraft();
    if (
      !dirty &&
      route.value === "settings" &&
      draft.value?.web_port === value.settings.web_port
    )
      copyDraft();
  } catch (e) {
    if (id !== refreshID) return;
    if (e instanceof APIError && e.status === 401) {
      auth.value = { initialized: true, authenticated: false };
    } else {
      error.value = message(e);
    }
  } finally {
    if (id === refreshID) {
      refreshing = false;
      loading.value = false;
    }
  }
}
async function loadNodes() {
  const id = ++nodeID;
  nodeError.value = "";
  try {
    const value = await api<{ proxies: Record<string, Proxy> }>("/nodes");
    if (id !== nodeID) return;
    proxies.value = value.proxies;
    if (!proxies.value[group.value]?.all)
      group.value = groups.value[0]?.name || "";
  } catch (e) {
    if (id === nodeID) nodeError.value = message(e);
  }
}
async function mutate(task: () => Promise<unknown>, text: string) {
  if (busy.value) return false;
  busy.value = true;
  error.value = "";
  success.value = "";
  try {
    await task();
    success.value = text;
    await refresh(true);
    if (route.value === "nodes" && state.value?.service.running)
      await loadNodes();
    return true;
  } catch (e) {
    error.value = message(e);
    if (e instanceof APIError && e.status === 401)
      auth.value = { initialized: true, authenticated: false };
    return false;
  } finally {
    busy.value = false;
  }
}
function ask(
  title: string,
  text: string,
  label: string,
  action: () => Promise<boolean | void>,
) {
  confirmTitle.value = title;
  confirmText.value = text;
  confirmLabel.value = label;
  confirmAction.value = action;
  dialog.value = "confirm";
}
async function confirmOperation() {
  if (!confirmAction.value) return;
  const result = await confirmAction.value();
  if (result !== false) dialog.value = "";
}
function syncHash() {
  const parts = location.hash.slice(1).split("?");
  const id = parts[0] || "overview";
  route.value = pages.some((p) => p.id === id) ? id : "overview";
  const p = new URLSearchParams(parts[1]);
  query.value = p.get("q") || "";
  page.value = Math.max(1, Number(p.get("page")) || 1);
}
function writeHash() {
  const p = new URLSearchParams();
  if (query.value) p.set("q", query.value);
  if (page.value > 1) p.set("page", String(page.value));
  history.replaceState(
    null,
    "",
    "#" + route.value + (p.size ? "?" + p.toString() : ""),
  );
}
function navigate(id: string) {
  if (id === route.value) return;
  const go = () => {
    if (route.value === "settings") copyDraft();
    location.hash = id;
  };
  if (isDirty.value && route.value === "settings")
    ask(
      "放弃未保存的设置？",
      "离开后将丢弃本页的设置修改。",
      "放弃修改",
      async () => go(),
    );
  else go();
}
function beforeUnload(e: BeforeUnloadEvent) {
  if (isDirty.value) {
    e.preventDefault();
    e.returnValue = "";
  }
}
function routeChanged() {
  if (isDirty.value && route.value === "settings") {
    const target = location.hash;
    history.replaceState(null, "", "#settings");
    ask(
      "放弃未保存的设置？",
      "离开后将丢弃本页的设置修改。",
      "放弃修改",
      async () => {
        copyDraft();
        location.hash = target;
      },
    );
    return;
  }
  syncHash();
}
function searchInput(e: Event) {
  query.value = (e.target as HTMLInputElement).value;
  page.value = 1;
  if (!composing.value) writeHash();
}
async function clearSearch() {
  query.value = "";
  page.value = 1;
  writeHash();
  await nextTick();
  document.getElementById("list-search")?.focus();
}
watch(route, async () => {
  document.title = "Meshport · " + title.value;
  error.value = "";
  if (route.value === "nodes") await loadNodes();
});
watch(totalPages, (n) => {
  if (page.value > n) {
    page.value = n;
    writeHash();
  }
});
watch(group, () => {
  page.value = 1;
  writeHash();
});
async function login() {
  const rules: Record<string, (v: unknown) => string> = {
    authPassword: (v) =>
      auth.value?.initialized
        ? String(v)
          ? ""
          : "请输入密码"
        : passwordRule(v),
  };
  if (!auth.value?.initialized)
    rules.initToken = (v) =>
      String(v).trim() ? "" : "请输入安装时生成的一次性凭证";
  if (
    !(await form.validate(
      { authPassword: authPassword.value, initToken: initToken.value },
      rules,
    ))
  )
    return;
  const ok = await mutate(
    () =>
      api(auth.value?.initialized ? "/login" : "/setup", "POST", {
        password: authPassword.value,
        ...(!auth.value?.initialized ? { token: initToken.value.trim() } : {}),
      }),
    "登录成功",
  );
  if (ok) {
    auth.value = { initialized: true, authenticated: true };
    authPassword.value = "";
    initToken.value = "";
    await refresh(true);
  }
}
async function logout() {
  if (await mutate(() => api("/logout", "POST"), "已退出登录")) {
    auth.value = { initialized: true, authenticated: false };
    state.value = null;
    draft.value = null;
    proxies.value = {};
  }
}
async function addSubscription() {
  const rules: Record<string, (v: unknown) => string> = {
    newName: (v) => (String(v).trim() ? "" : "请输入订阅名称"),
  };
  if (importMode.value === "url")
    rules.newURL = (v) =>
      /^https?:\/\//.test(String(v)) ? "" : "请输入 HTTP(S) 订阅地址";
  else
    rules.newContent = (v) =>
      String(v).trim() ? "" : "请粘贴 Clash / Mihomo YAML";
  if (
    !(await form.validate(
      {
        newName: newName.value,
        newURL: newURL.value,
        newContent: newContent.value,
      },
      rules,
    ))
  )
    return;
  if (
    await mutate(
      () =>
        api("/subscriptions", "POST", {
          name: newName.value,
          ...(importMode.value === "url"
            ? { url: newURL.value }
            : { content: newContent.value }),
        }),
      "订阅已添加，请启用后启动代理",
    )
  ) {
    dialog.value = "";
    newName.value = "";
    newURL.value = "";
    newContent.value = "";
  }
}
function deleteSubscription(sub: Subscription) {
  ask(
    "删除「" + sub.name + "」？",
    "将删除这个订阅及其缓存和节点选择记录。此操作无法撤销。",
    "删除订阅",
    () => mutate(() => api("/subscriptions/" + sub.id, "DELETE"), "订阅已删除"),
  );
}
async function subscriptionAction(sub: Subscription, action: string) {
  await mutate(
    () => api("/subscriptions/" + sub.id + "/" + action, "POST"),
    action === "activate" ? "订阅已启用" : "订阅已更新",
  );
}
async function serviceAction(action: string) {
  await mutate(
    () => api("/service", "POST", { action }),
    { start: "代理已启动", stop: "代理已停止", restart: "代理已重启" }[
      action
    ] || "操作完成",
  );
}
async function saveSettings(confirmed = false) {
  if (!draft.value || !state.value) return;
  const rules: Record<string, (v: unknown) => string> = {
    webPort: (v) => portRule(v),
    httpPort: (v) => portRule(v),
    socksPort: (v) => portRule(v),
  };
  if (draft.value.allow_lan) {
    rules.proxyUser = (v) => (String(v).trim() ? "" : "外部访问需要代理用户名");
    if (!draft.value.has_proxy_password)
      rules.proxyPassword = (v) =>
        String(v).length >= 8 ? "" : "代理密码至少 8 位";
  }
  if (
    !(await form.validate(
      {
        webPort: draft.value.web_port,
        httpPort: draft.value.http_port,
        socksPort: draft.value.socks_port,
        proxyUser: draft.value.proxy_user,
        proxyPassword: draft.value.proxy_password,
      },
      rules,
    ))
  )
    return;
  if (
    !confirmed &&
    ((draft.value.tun && !state.value.settings.tun) ||
      (draft.value.allow_lan && !state.value.settings.allow_lan))
  ) {
    ask(
      "确认网络设置",
      "开启 TUN 后，需在 60 秒内确认后台可访问，否则自动恢复。开启外部代理访问后，其他主机可凭代理用户名和密码使用本机代理。",
      "保存并应用",
      () => saveSettings(true),
    );
    return;
  }
  const oldPort = state.value.settings.web_port,
    newPort = Number(draft.value.web_port);
  const settings = {
    ...draft.value,
    web_port: newPort,
    http_port: Number(draft.value.http_port),
    socks_port: Number(draft.value.socks_port),
    bypass: bypassText.value
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean),
  };
  delete settings.has_proxy_password;
  const ok = await mutate(
    () => api("/settings", "PUT", { settings, revision: draftRevision.value }),
    "设置已保存",
  );
  if (ok) {
    copyDraft();
    if (oldPort !== newPort) {
      const url = new URL(location.href);
      const browserPort = Number(
        url.port || (url.protocol === "https:" ? 443 : 80),
      );
      if (url.protocol === "https:" || browserPort !== oldPort) {
        success.value = `后台监听端口已切换至 ${newPort}，请同步反向代理上游后刷新本页`;
      } else {
        url.port = String(newPort);
        success.value = "新端口已启用，请使用新地址继续管理";
      }
      newAddress.value = url.href;
    }
    return true;
  }
  return false;
}
function portRule(v: unknown) {
  const p = Number(v);
  return Number.isInteger(p) && p >= 1024 && p <= 65535
    ? ""
    : "请输入 1024–65535 范围内的整数端口";
}
const newAddress = ref("");
async function changePassword() {
  if (
    !(await form.validate(
      {
        currentPassword: currentPassword.value,
        newPassword: newPassword.value,
      },
      {
        currentPassword: (v) => (String(v) ? "" : "请输入当前密码"),
        newPassword: passwordRule,
      },
    ))
  )
    return;
  if (
    await mutate(
      () =>
        api("/password", "PUT", {
          current: currentPassword.value,
          password: newPassword.value,
        }),
      "密码已更新，请重新登录",
    )
  ) {
    dialog.value = "";
    currentPassword.value = "";
    newPassword.value = "";
    auth.value = { initialized: true, authenticated: false };
  }
}
async function selectNode(name: string) {
  await mutate(
    () => api("/nodes/select", "PUT", { group: group.value, name }),
    "节点已切换",
  );
}
onMounted(async () => {
  syncHash();
  document.title = "Meshport · " + title.value;
  window.addEventListener("hashchange", routeChanged);
  window.addEventListener("beforeunload", beforeUnload);
  try {
    auth.value = await api("/auth");
    if (auth.value?.authenticated) {
      await refresh(true);
      if (route.value === "nodes") await loadNodes();
    }
  } catch (e) {
    error.value = message(e);
  } finally {
    loading.value = false;
  }
  poll = setInterval(() => {
    if (auth.value?.authenticated && !busy.value && !newAddress.value) {
      void refresh();
      if (route.value === "nodes" && state.value?.service.running)
        void loadNodes();
    }
  }, 3000);
  clock = setInterval(() => (now.value = Date.now()), 500);
});
onBeforeUnmount(() => {
  clearInterval(poll);
  clearInterval(clock);
  window.removeEventListener("hashchange", routeChanged);
  window.removeEventListener("beforeunload", beforeUnload);
});
</script>

<template>
  <div v-if="!auth?.authenticated" class="auth-shell">
    <div class="auth-intro">
      <div class="brand"><Cable /><span>Meshport</span></div>
      <h1>让服务器的代理<br />清楚、可控。</h1>
      <p>一个轻量的控制台。管理订阅、选择节点，<br />让每一条连接各行其道。</p>
      <div class="auth-lines">
        <span>管理后台</span><span>HTTP / SOCKS5</span
        ><span>系统流量 / TUN</span>
      </div>
      <small>为 Linux 服务器而生</small>
    </div>
    <main class="auth-card">
      <div class="eyebrow">服务器控制台</div>
      <h2>{{ auth?.initialized ? "登录控制台" : "初始化 Meshport" }}</h2>
      <p>
        {{
          auth?.initialized
            ? "输入管理员密码，继续管理你的服务器。"
            : "使用安装时的一次性凭证，设置管理员密码。"
        }}
      </p>
      <div v-if="loading" class="loading">
        <LoaderCircle class="spin" />正在连接后台…
      </div>
      <form v-else novalidate @submit.prevent="login">
        <SecretField
          v-if="!auth?.initialized"
          id="initToken"
          v-model="initToken"
          label="一次性初始化凭证"
          :error="errors.initToken"
          hint="从安装输出或 init-token 文件中获取"
        /><SecretField
          id="authPassword"
          v-model="authPassword"
          :label="auth?.initialized ? '管理员密码' : '设置管理员密码'"
          :autocomplete="
            auth?.initialized ? 'current-password' : 'new-password'
          "
          :error="errors.authPassword"
          :hint="auth?.initialized ? '' : '至少 12 个字符，支持密码管理器'"
        />
        <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
        <button
          class="primary full"
          :disabled="busy || !auth"
          :aria-busy="busy"
          type="submit"
        >
          <LoaderCircle v-if="busy" class="spin" />{{
            auth?.initialized ? "登录" : "设置密码并进入"
          }}<ArrowRight v-if="!busy" />
        </button>
      </form>
      <small class="auth-note"
        ><ShieldCheck />管理员凭证与代理访问凭证独立</small
      >
    </main>
  </div>
  <div v-else class="app-shell">
    <aside class="sidebar">
      <a class="brand" href="#overview" @click.prevent="navigate('overview')"
        ><Cable /><span>Meshport</span></a
      >
      <p class="sidebar-caption">服务器代理控制台</p>
      <nav aria-label="主导航">
        <a
          v-for="item in pages"
          :key="item.id"
          :href="'#' + item.id"
          :aria-current="route === item.id ? 'page' : undefined"
          :class="{ active: route === item.id }"
          @click.prevent="navigate(item.id)"
          ><component :is="item.icon" />{{ item.name }}</a
        >
      </nav>
      <div class="sidebar-bottom">
        <div class="management"><span class="dot" />管理服务在线</div>
        <p>代理停止，后台依然可用。</p>
        <button class="ghost" :disabled="busy" @click="logout">
          <LogOut />退出登录
        </button>
      </div>
    </aside>
    <main class="workspace">
      <header class="page-heading">
        <div>
          <div class="eyebrow">MESHPORT / {{ title }}</div>
          <h1>{{ title }}</h1>
          <p>
            {{
              {
                overview: "连接状态与代理控制，一目了然。",
                subscriptions: "保存多份订阅，每次启用一份。",
                nodes: "为当前策略组选择合适的连接。",
                settings: "按照服务器的需要，配置每一个入口。",
              }[route]
            }}
          </p>
        </div>
        <button
          class="outline"
          :disabled="busy || loading"
          @click="refresh(true)"
        >
          <RefreshCw :class="{ spin: loading }" />刷新状态
        </button>
      </header>
      <div class="feedback" aria-live="polite" role="status">
        <span v-if="busy"><LoaderCircle class="spin" />正在处理，请稍候…</span
        ><span v-else-if="success"><Check />{{ success }}</span>
      </div>
      <div v-if="error" class="alert danger" role="alert">
        <span>{{ error }}</span
        ><button
          type="button"
          class="icon-button"
          aria-label="关闭错误提示"
          @click="error = ''"
        >
          <X />
        </button>
      </div>
      <div v-if="newAddress" class="alert info">
        <a :href="newAddress">{{ newAddress }} <ExternalLink /></a
        ><span>反向代理用户需同步修改上游端口。</span>
      </div>
      <div v-if="state?.notice" class="alert warning" role="status">
        {{ state.notice }}
      </div>
      <div
        v-if="state?.pending_tun"
        class="alert warning tun-pending"
        role="alert"
      >
        <div>
          <strong>TUN 等待确认 · {{ seconds }} 秒</strong>
          <p>确认 SSH、后台和 DNS 正常。未确认将自动恢复原配置。</p>
        </div>
        <button
          class="primary"
          :disabled="busy || seconds === 0"
          @click="mutate(() => api('/tun/confirm', 'POST'), 'TUN 已确认')"
        >
          确认后台可访问
        </button>
      </div>
      <div v-if="loading || !state" class="loading panel">
        <LoaderCircle class="spin" />正在读取服务器状态…
      </div>
      <template v-else>
        <section v-if="route === 'overview'" class="overview-grid">
          <div class="panel control-panel">
            <div class="panel-top">
              <span class="eyebrow">代理内核</span
              ><span
                class="badge"
                :class="state.service.running ? 'positive' : 'neutral'"
                ><span class="dot" />{{
                  state.service.running ? "运行中" : "已停止"
                }}</span
              >
            </div>
            <h2>{{ state.service.running ? "连接已就绪" : "准备好再连接" }}</h2>
            <p>
              {{
                state.service.running
                  ? "应用可通过 HTTP 或 SOCKS5 使用代理。"
                  : "添加并启用有效订阅后，手动启动代理。"
              }}
            </p>
            <div class="active-sub">
              <Layers />
              <div>
                <small>当前订阅</small
                ><strong>{{ activeSub?.name || "尚未启用订阅" }}</strong>
              </div>
            </div>
            <div class="button-row">
              <button
                class="primary"
                :disabled="
                  busy ||
                  !activeSub ||
                  !state.service.available ||
                  !!state.pending_tun
                "
                @click="
                  serviceAction(state.service.running ? 'restart' : 'start')
                "
              >
                <RotateCw v-if="state.service.running" /><Play v-else />{{
                  state.service.running ? "重启代理" : "启动代理"
                }}</button
              ><button
                v-if="state.service.running"
                class="outline"
                :disabled="busy"
                @click="serviceAction('stop')"
              >
                <Square />停止代理</button
              ><a
                v-else-if="!activeSub"
                href="#subscriptions"
                @click.prevent="navigate('subscriptions')"
                >前往添加订阅 <ArrowRight
              /></a>
            </div>
            <small v-if="!state.service.available" class="invalid"
              >服务控制不可用，请检查辅助程序权限。</small
            >
          </div>
          <div class="panel connection-panel">
            <div class="panel-top">
              <h2>连接路径</h2>
              <Cable />
            </div>
            <div class="path-row">
              <span class="path-source">浏览器</span
              ><span class="path-line connected" />
              <div>
                <strong>管理后台</strong
                ><small class="mono"
                  >:{{ state.settings.web_port }} · 始终独立运行</small
                >
              </div>
            </div>
            <div class="path-row">
              <span class="path-source">应用</span
              ><span
                class="path-line"
                :class="{ connected: state.service.running }"
              />
              <div>
                <strong>显式代理</strong
                ><small>{{
                  state.service.running
                    ? "HTTP / SOCKS5 已就绪"
                    : "等待代理启动"
                }}</small>
              </div>
            </div>
            <div class="path-row">
              <span class="path-source">系统</span
              ><span
                class="path-line"
                :class="{
                  connected: state.settings.tun && state.service.running,
                }"
              />
              <div>
                <strong>TUN 接管</strong
                ><small>{{
                  state.settings.tun ? "已开启" : "未开启 · 不接管系统流量"
                }}</small>
              </div>
            </div>
          </div>
          <div class="panel endpoints">
            <h2>代理入口</h2>
            <dl>
              <div>
                <dt>HTTP</dt>
                <dd class="mono">
                  {{ state.settings.allow_lan ? "0.0.0.0" : "127.0.0.1" }}:{{
                    state.settings.http_port
                  }}
                </dd>
              </div>
              <div>
                <dt>SOCKS5</dt>
                <dd class="mono">
                  {{ state.settings.allow_lan ? "0.0.0.0" : "127.0.0.1" }}:{{
                    state.settings.socks_port
                  }}
                </dd>
              </div>
            </dl>
            <p>
              {{
                state.settings.allow_lan
                  ? "允许外部访问，需要独立代理认证。"
                  : "仅本机访问，在应用中显式配置代理。"
              }}
            </p>
          </div>
          <div class="panel">
            <h2>运行策略</h2>
            <dl>
              <div>
                <dt>代理模式</dt>
                <dd>
                  {{
                    { rule: "规则", global: "全局", direct: "直连" }[
                      state.settings.mode
                    ]
                  }}
                </dd>
              </div>
              <div>
                <dt>代理开机自启</dt>
                <dd>{{ state.service.autostart ? "已开启" : "已关闭" }}</dd>
              </div>
              <div>
                <dt>订阅数量</dt>
                <dd>{{ state.subscriptions.length }}</dd>
              </div>
            </dl>
            <a href="#settings" @click.prevent="navigate('settings')"
              >调整设置 <ArrowRight
            /></a>
          </div>
        </section>
        <section v-if="route === 'subscriptions'" class="panel list-panel">
          <div class="panel-top">
            <div>
              <h2>
                订阅列表
                <span class="count">{{ state.subscriptions.length }}</span>
              </h2>
              <p>支持完整配置或节点列表的 Clash / Mihomo YAML。</p>
            </div>
            <button
              class="primary"
              :disabled="busy"
              @click="dialog = 'subscription'"
            >
              <Plus />添加订阅
            </button>
          </div>
          <div class="search" v-if="state.subscriptions.length">
            <Search /><input
              id="list-search"
              type="search"
              aria-label="搜索订阅"
              placeholder="搜索订阅名称"
              :value="query"
              @input="searchInput"
              @compositionstart="composing = true"
              @compositionend="
                composing = false;
                writeHash();
              "
            /><button
              v-if="query"
              type="button"
              class="icon-button"
              aria-label="清除搜索"
              @click="clearSearch"
            >
              <X />
            </button>
          </div>
          <div v-if="!state.subscriptions.length" class="empty">
            <Layers />
            <h3>从第一份订阅开始</h3>
            <p>添加订阅地址或粘贴 YAML，验证通过后即可启用。</p>
            <button
              class="outline"
              :disabled="busy"
              @click="dialog = 'subscription'"
            >
              添加订阅
            </button>
          </div>
          <div v-else-if="!filtered.length" class="empty">
            <Search />
            <h3>没有匹配的订阅</h3>
            <button class="outline" @click="clearSearch">清除搜索</button>
          </div>
          <article
            v-for="sub in visible as Subscription[]"
            :key="sub.id"
            class="subscription-row"
          >
            <div class="subscription-symbol"><Layers /></div>
            <div class="subscription-info">
              <div>
                <h3>{{ sub.name }}</h3>
                <span v-if="sub.active" class="badge positive">当前启用</span>
              </div>
              <p>
                {{ sub.source }} ·
                {{ sub.kind === "full" ? "完整配置" : "节点列表" }}
              </p>
              <small>更新于 {{ date(sub.updated_at) }} · {{ timeZone }}</small>
            </div>
            <div class="row-actions">
              <button
                class="outline"
                :disabled="busy || sub.active || !!state.pending_tun"
                @click="subscriptionAction(sub, 'activate')"
              >
                {{ sub.active ? "已启用" : "启用" }}</button
              ><button
                class="icon-button"
                :disabled="busy || sub.source === '手动导入'"
                :title="
                  sub.source === '手动导入'
                    ? '手动导入没有更新地址'
                    : '更新订阅'
                "
                :aria-label="'更新订阅 ' + sub.name"
                @click="subscriptionAction(sub, 'update')"
              >
                <RefreshCw /></button
              ><button
                class="icon-button danger-text"
                :disabled="busy || !!state.pending_tun"
                :aria-label="'删除订阅 ' + sub.name"
                @click="deleteSubscription(sub)"
              >
                <Trash2 />
              </button>
            </div>
          </article>
        </section>
        <section v-if="route === 'nodes'" class="panel">
          <div class="panel-top">
            <div>
              <h2>{{ activeSub?.name || "当前订阅" }}的节点</h2>
              <p>只有手动选择策略组可以切换；自动策略组展示当前结果。</p>
            </div>
            <button
              class="outline"
              :disabled="busy || !state.service.running"
              @click="loadNodes"
            >
              <RefreshCw />刷新节点
            </button>
          </div>
          <div v-if="!state.service.running" class="empty">
            <Network />
            <h3>内核已停止</h3>
            <p>启动代理后，即可查看策略组与节点状态。</p>
            <button
              class="primary"
              :disabled="busy || !activeSub"
              @click="serviceAction('start')"
            >
              启动代理
            </button>
          </div>
          <div v-else-if="nodeError" class="alert danger" role="alert">
            {{ nodeError }}
          </div>
          <template v-else
            ><SelectField
              id="proxy-group"
              v-model="group"
              label="策略组"
              :options="
                groups.map((g) => ({
                  value: g.name,
                  label:
                    g.name +
                    ' · ' +
                    (g.type === 'Selector' ? '手动选择' : g.type),
                }))
              "
            />
            <p v-if="currentGroup" class="group-result">
              当前选择：<strong>{{
                currentGroup.now || "由内核自动分配"
              }}</strong>
            </p>
            <div class="search">
              <Search /><input
                id="list-search"
                type="search"
                aria-label="搜索节点"
                placeholder="搜索节点名称"
                :value="query"
                @input="searchInput"
                @compositionstart="composing = true"
                @compositionend="
                  composing = false;
                  writeHash();
                "
              /><button
                v-if="query"
                class="icon-button"
                type="button"
                aria-label="清除搜索"
                @click="clearSearch"
              >
                <X />
              </button>
            </div>
            <div v-if="!filtered.length" class="empty">
              <h3>没有匹配的节点</h3>
              <button v-if="query" class="outline" @click="clearSearch">
                清除搜索
              </button>
            </div>
            <div class="node-grid">
              <button
                v-for="node in visible as Proxy[]"
                :key="node.name"
                class="node"
                :class="{ selected: currentGroup?.now === node.name }"
                :disabled="
                  busy ||
                  currentGroup?.type !== 'Selector' ||
                  !!state.pending_tun
                "
                :aria-pressed="currentGroup?.now === node.name"
                @click="selectNode(node.name)"
              >
                <div>
                  <strong>{{ node.name }}</strong
                  ><Check v-if="currentGroup?.now === node.name" />
                </div>
                <span
                  >{{ node.type || "节点" }}
                  <span v-if="node.history?.length" class="mono"
                    >· {{ node.history.at(-1)?.delay || "--" }} ms</span
                  ></span
                >
              </button>
            </div></template
          >
        </section>
        <section v-if="route === 'settings' && draft" class="settings-layout">
          <form
            novalidate
            @submit.prevent="saveSettings()"
            class="settings-form"
          >
            <div class="panel">
              <h2>代理设置</h2>
              <p>模式决定流量分流；TUN 决定是否接管系统流量。</p>
              <ModeControl v-model="draft.mode" :disabled="busy" />
              <div class="field-grid">
                <Field
                  id="httpPort"
                  v-model="draft.http_port"
                  label="HTTP 端口"
                  type="number"
                  :error="errors.httpPort"
                /><Field
                  id="socksPort"
                  v-model="draft.socks_port"
                  label="SOCKS5 端口"
                  type="number"
                  :error="errors.socksPort"
                />
              </div>
              <label class="toggle-row"
                ><div>
                  <strong>代理开机自启</strong><small>独立于后台开机自启</small>
                </div>
                <input
                  type="checkbox"
                  v-model="draft.autostart"
                  :disabled="busy" /></label
              ><label class="toggle-row"
                ><div>
                  <strong>允许外部代理访问</strong
                  ><small>需要独立代理用户名与密码</small>
                </div>
                <input
                  type="checkbox"
                  v-model="draft.allow_lan"
                  :disabled="busy"
              /></label>
              <div v-if="draft.allow_lan" class="field-grid">
                <Field
                  id="proxyUser"
                  v-model="draft.proxy_user"
                  label="代理用户名"
                  :error="errors.proxyUser"
                /><SecretField
                  id="proxyPassword"
                  v-model="draft.proxy_password!"
                  label="代理密码"
                  autocomplete="new-password"
                  :error="errors.proxyPassword"
                  :hint="
                    draft.has_proxy_password
                      ? '留空保留已保存的密码'
                      : '至少 8 位，与管理员密码独立'
                  "
                />
              </div>
            </div>
            <div class="panel">
              <h2>TUN 与绕行</h2>
              <p>开启前确认服务器网络环境。保存后需在 60 秒内确认可访问。</p>
              <label class="toggle-row"
                ><div>
                  <strong>接管系统流量</strong
                  ><small>{{
                    !state.service.running
                      ? "开启 TUN 前请先启动代理"
                      : "使用独立的 TUN 网络设备"
                  }}</small>
                </div>
                <input
                  type="checkbox"
                  v-model="draft.tun"
                  :disabled="busy || (!state.service.running && !draft.tun)"
              /></label>
              <div class="field">
                <label for="bypass">额外绕行网段</label
                ><textarea
                  id="bypass"
                  v-model="bypassText"
                  rows="5"
                  style="resize: none"
                  aria-describedby="bypass-help"
                  placeholder="203.0.113.5/32&#10;198.51.100.0/24"
                ></textarea
                ><small id="bypass-help"
                  >每行一个
                  CIDR。私有网络、本机和当前后台来源地址自动绕行。</small
                >
              </div>
            </div>
            <div class="panel">
              <h2>管理后台</h2>
              <Field
                id="webPort"
                v-model="draft.web_port"
                label="Web 端口"
                type="number"
                :error="errors.webPort"
                hint="新端口成功监听后关闭旧端口；反向代理需同步修改上游"
              /><button
                type="button"
                class="outline"
                :disabled="busy"
                @click="dialog = 'password'"
              >
                修改管理员密码
              </button>
            </div>
            <div class="save-bar">
              <span>{{ isDirty ? "有未保存的修改" : "设置已同步" }}</span
              ><button
                class="outline"
                type="button"
                :disabled="busy || !isDirty"
                @click="copyDraft"
              >
                放弃修改</button
              ><button
                class="primary"
                type="submit"
                :disabled="busy || !isDirty || !!state.pending_tun"
                :aria-busy="busy"
              >
                保存设置
              </button>
            </div>
          </form>
          <aside class="settings-help">
            <ShieldCheck />
            <h3>管理连接优先</h3>
            <p>后台与代理内核独立运行。停止代理不会关闭管理后台。</p>
            <p>TUN 未确认时会自动恢复。如果无法访问，可通过 SSH 停止内核：</p>
            <code>sudo systemctl stop<br />meshport-mihomo</code>
            <p>然后回到后台，关闭 TUN 并保存，再启动代理。</p>
          </aside>
        </section>
        <footer
          v-if="
            (route === 'subscriptions' || route === 'nodes') && filtered.length
          "
          class="pagination"
        >
          <span
            >共 {{ filtered.length }} 项 · 第 {{ page }} /
            {{ totalPages }} 页</span
          >
          <div>
            <button
              class="outline"
              :disabled="page <= 1"
              @click="
                page--;
                writeHash();
              "
            >
              上一页</button
            ><button
              class="outline"
              :disabled="page >= totalPages"
              @click="
                page++;
                writeHash();
              "
            >
              下一页
            </button>
          </div>
        </footer>
      </template>
      <footer class="app-footer">
        Meshport <span>轻量管理 · 清晰连接</span>
      </footer>
    </main>
  </div>
  <Dialog
    :open="!!dialog"
    :title="
      dialog === 'subscription'
        ? '添加订阅'
        : dialog === 'password'
          ? '修改管理员密码'
          : confirmTitle
    "
    :busy="busy"
    @close="dialog = ''"
  >
    <form
      v-if="dialog === 'subscription'"
      novalidate
      @submit.prevent="addSubscription"
    >
      <Field
        id="newName"
        v-model="newName"
        label="订阅名称"
        :error="errors.newName"
      />
      <fieldset class="mode-control">
        <legend>导入方式</legend>
        <label :class="{ selected: importMode === 'url' }"
          ><input
            type="radio"
            name="import-mode"
            v-model="importMode"
            value="url"
          />订阅 URL</label
        ><label :class="{ selected: importMode === 'yaml' }"
          ><input
            type="radio"
            name="import-mode"
            v-model="importMode"
            value="yaml"
          />粘贴 YAML</label
        >
      </fieldset>
      <SecretField
        v-if="importMode === 'url'"
        id="newURL"
        v-model="newURL"
        label="订阅地址"
        :error="errors.newURL"
        hint="仅支持 Clash / Mihomo YAML 订阅"
      />
      <div v-else class="field">
        <label for="newContent">Clash / Mihomo YAML</label
        ><textarea
          id="newContent"
          v-model="newContent"
          rows="10"
          style="resize: none"
          :aria-invalid="!!errors.newContent"
          aria-describedby="newContent-help"
        ></textarea
        ><small id="newContent-help" :class="{ invalid: errors.newContent }">{{
          errors.newContent || "最大 8 MiB，不支持 Base64 或节点 URI"
        }}</small>
      </div>
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div class="dialog-actions">
        <button
          type="button"
          data-safe-focus
          class="outline"
          :disabled="busy"
          @click="dialog = ''"
        >
          取消</button
        ><button type="submit" class="primary" :disabled="busy">
          添加订阅
        </button>
      </div>
    </form>
    <form
      v-else-if="dialog === 'password'"
      novalidate
      @submit.prevent="changePassword"
    >
      <SecretField
        id="currentPassword"
        v-model="currentPassword"
        label="当前密码"
        autocomplete="current-password"
        :error="errors.currentPassword"
      /><SecretField
        id="newPassword"
        v-model="newPassword"
        label="新密码"
        autocomplete="new-password"
        :error="errors.newPassword"
        hint="保存后所有会话失效，需要重新登录"
      />
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div class="dialog-actions">
        <button
          type="button"
          data-safe-focus
          class="outline"
          :disabled="busy"
          @click="dialog = ''"
        >
          取消</button
        ><button type="submit" class="primary" :disabled="busy">
          修改密码
        </button>
      </div>
    </form>
    <template v-else
      ><p>{{ confirmText }}</p>
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div class="dialog-actions">
        <button
          type="button"
          class="outline"
          data-safe-focus
          :disabled="busy"
          @click="dialog = ''"
        >
          取消</button
        ><button
          type="button"
          :class="confirmLabel.includes('删除') ? 'danger-button' : 'primary'"
          :disabled="busy"
          @click="confirmOperation"
        >
          {{ confirmLabel }}
        </button>
      </div></template
    >
  </Dialog>
</template>
