<script setup lang="ts">
defineProps<{
  id: string;
  label: string;
  modelValue: string;
  options: { value: string; label: string }[];
  disabled?: boolean;
}>();
defineEmits<{ "update:modelValue": [value: string] }>();
</script>
<template>
  <div class="field">
    <label :for="id">{{ label }}</label
    ><select
      :id="id"
      :value="modelValue"
      :disabled="disabled"
      @change="
        $emit('update:modelValue', ($event.target as HTMLSelectElement).value)
      "
    >
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>
  </div>
</template>
