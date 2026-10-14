export type Settings = {
  web_port: number;
  http_port: number;
  socks_port: number;
  mode: string;
  allow_lan: boolean;
  proxy_user: string;
  proxy_password?: string;
  has_proxy_password?: boolean;
  autostart: boolean;
  tun: boolean;
  bypass: string[];
};
export type Subscription = {
  id: string;
  name: string;
  source: string;
  kind: string;
  active: boolean;
  updated_at: string;
};
export type State = {
  revision: number;
  settings: Settings;
  subscriptions: Subscription[];
  active: string;
  service: {
    running: boolean;
    autostart: boolean;
    available: boolean;
    driver: string;
  };
  pending_tun: { deadline: string } | null;
  notice: string;
};
export type Proxy = {
  name: string;
  type: string;
  now?: string;
  all?: string[];
  history?: { delay: number }[];
};
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 60000);
  try {
    const res = await fetch("/api" + path, {
      method,
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-Meshport-Request": "1",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: signal
        ? AbortSignal.any([controller.signal, signal])
        : controller.signal,
    });
    const value = await res.json();
    if (!res.ok)
      throw new APIError(value.error || "操作失败，请重试", res.status);
    return value;
  } catch (e) {
    if (signal?.aborted) throw e;
    if (e instanceof APIError) throw e;
    throw new APIError("无法连接后台，请检查网络后重试", 0);
  } finally {
    clearTimeout(timer);
  }
}
