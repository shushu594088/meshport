<script setup lang="ts">
import { ref } from "vue";
import { Eye, EyeOff } from "lucide-vue-next";
defineProps<{
  id: string;
  label: string;
  modelValue: string;
  error?: string;
  hint?: string;
  autocomplete?: string;
  disabled?: boolean;
}>();
defineEmits<{ "update:modelValue": [value: string] }>();
const revealed = ref(false);
</script>
<template>
  <div class="field">
    <label :for="id">{{ label }}</label>
    <div class="secret">
      <input
        :id="id"
        :type="revealed ? 'text' : 'password'"
        :value="modelValue"
        :autocomplete="autocomplete || 'off'"
        :disabled="disabled"
        :aria-invalid="!!error"
        :aria-describedby="id + '-help'"
        @input="
          $emit('update:modelValue', ($event.target as HTMLInputElement).value)
        "
      /><button
        type="button"
        class="reveal"
        :aria-label="(revealed ? '隐藏' : '显示') + label"
        :aria-pressed="revealed"
        @click="revealed = !revealed"
      >
        <EyeOff v-if="revealed" /><Eye v-else />
      </button>
    </div>
    <small :id="id + '-help'" :class="{ invalid: error }">{{
      error || hint || "\u00a0"
    }}</small>
  </div>
</template>
