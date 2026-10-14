import { reactive, watch } from "vue";
const defaults = {
  theme: "system",
  startPage: "overview",
  environment: "bash",
};
let stored = {};
try {
  stored = JSON.parse(localStorage.getItem("meshport.preferences") || "{}");
} catch {
  /* Storage can be disabled. */
}
export const preferences = reactive({ ...defaults, ...stored });
const media = matchMedia("(prefers-color-scheme: dark)");
function apply() {
  if (!["system", "light", "dark"].includes(preferences.theme))
    preferences.theme = "system";
  document.documentElement.dataset.theme =
    preferences.theme === "system"
      ? media.matches
        ? "dark"
        : "light"
      : preferences.theme;
}
apply();
media.addEventListener("change", apply);
watch(preferences, () => {
  apply();
  try {
    localStorage.setItem("meshport.preferences", JSON.stringify(preferences));
  } catch {
    /* Preferences still work for this tab. */
  }
});
