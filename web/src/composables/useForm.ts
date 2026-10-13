import { reactive, nextTick } from "vue";
export function useForm() {
  const errors = reactive<Record<string, string>>({});
  async function validate(
    values: Record<string, unknown>,
    rules: Record<string, (v: unknown) => string>,
  ) {
    Object.keys(errors).forEach((k) => delete errors[k]);
    for (const [key, rule] of Object.entries(rules)) {
      const error = rule(values[key]);
      if (error) errors[key] = error;
    }
    const first = Object.keys(errors)[0];
    if (first) {
      await nextTick();
      document.getElementById(first)?.focus();
      return false;
    }
    return true;
  }
  return { errors, validate };
}
export function passwordRule(v: unknown) {
  const s = String(v ?? "");
  return [...s].length < 12 || new TextEncoder().encode(s).length > 72
    ? "密码至少 12 个字符，且不超过 72 字节"
    : "";
}
