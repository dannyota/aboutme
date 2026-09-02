<script setup lang="ts">
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
} from '@/components/ui/card';

defineProps<{
  readonly title: string;
  readonly description?: string;
  readonly titleId?: string;
}>();
</script>

<template>
  <main
    class="mx-auto flex w-full max-w-md flex-col px-5 py-10
      sm:py-16"
  >
    <Card>
      <CardHeader>
        <h1
          :id="titleId ?? 'auth-title'"
          class="text-2xl font-semibold tracking-tight"
        >
          {{ title }}
        </h1>
        <CardDescription v-if="description || $slots.description">
          <slot name="description">
            {{ description }}
          </slot>
        </CardDescription>
      </CardHeader>
      <CardContent class="grid gap-4">
        <slot />
      </CardContent>
      <CardFooter
        v-if="$slots.footer"
        class="flex flex-wrap justify-between gap-3 text-sm"
      >
        <slot name="footer" />
      </CardFooter>
    </Card>
  </main>
</template>
