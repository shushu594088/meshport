import { ref, onBeforeUnmount } from "vue";
import { api, APIError } from "../api";

export type DiagnosticResult = {
  status: "supported" | "unsupported" | "reachable" | "failed";
  detail: string;
  region?: string;
  delay: number;
  checked_at: string;
  revision: number;
};
export const diagnosticLabels = {
  supported: "支持",
  unsupported: "不支持",
  reachable: "可访问",
  failed: "测试失败",
};

export function useDiagnostics(expired: () => void) {
  const results = ref<Record<string, DiagnosticResult>>({});
  const pending = ref<Set<string>>(new Set());
  const running = ref(false),
    completed = ref(0),
    total = ref(0),
    notice = ref("");
  let controller: AbortController | null = null;
  function cancel() {
    if (!controller || !running.value) return;
    controller.abort();
    notice.value = "测试已取消，已完成的结果保留";
  }
  function reset() {
    cancel();
    results.value = {};
    notice.value = "";
  }
  async function run(kind: "delay" | "site", keys: string[]) {
    if (running.value || !keys.length) return;
    const current = new AbortController();
    controller = current;
    running.value = true;
    completed.value = 0;
    total.value = keys.length;
    notice.value = "";
    let index = 0;
    const worker = async () => {
      while (index < keys.length && !current.signal.aborted) {
        const key = keys[index++];
        pending.value.add(key);
        try {
          const result = await api<DiagnosticResult>(
            "/tests/" + kind,
            "POST",
            kind === "site" ? { id: key } : { name: key },
            current.signal,
          );
          if (!current.signal.aborted) results.value[key] = result;
        } catch (e) {
          if (current.signal.aborted) return;
          if (e instanceof APIError && e.status === 401) {
            expired();
            current.abort();
            return;
          }
          results.value[key] = {
            status: "failed",
            detail: e instanceof Error ? e.message : "测试失败，请重试",
            delay: 0,
            checked_at: new Date().toISOString(),
            revision: 0,
          };
        } finally {
          pending.value.delete(key);
          if (!current.signal.aborted) completed.value++;
        }
      }
    };
    try {
      await Promise.all(
        Array.from({ length: Math.min(4, keys.length) }, worker),
      );
    } finally {
      running.value = false;
      if (!current.signal.aborted)
        notice.value = `已完成 ${completed.value} 项测试`;
      controller = null;
    }
  }
  onBeforeUnmount(cancel);
  return {
    results,
    pending,
    running,
    completed,
    total,
    notice,
    run,
    cancel,
    reset,
  };
}
