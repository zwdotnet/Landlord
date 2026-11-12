<template>
  <Teleport to="body">
    <div ref="dialog" role="dialog" aria-modal="true" :aria-label="title"
      class="fixed inset-0 z-[70] bg-black/90 text-white flex flex-col p-3 sm:p-5"
      @keydown.esc.stop.prevent="emit('close')" @keydown.tab="trapFocus">
      <div class="flex flex-wrap items-center justify-between gap-2 pb-3">
        <span class="text-sm">{{ title }}</span>
        <div class="flex items-center gap-2 text-sm">
          <button type="button" class="viewer-button" aria-label="缩小图片" :disabled="zoom <= 1" @click="zoom = Math.max(1, zoom - 0.5)">−</button>
          <span class="w-12 text-center tabular-nums" aria-live="polite">{{ Math.round(zoom * 100) }}%</span>
          <button type="button" class="viewer-button" aria-label="放大图片" :disabled="zoom >= 4" @click="zoom = Math.min(4, zoom + 0.5)">＋</button>
          <button type="button" class="viewer-button" @click="rotation = (rotation + 90) % 360">旋转</button>
          <button type="button" class="viewer-button" @click="reset">还原</button>
          <button ref="closeButton" type="button" class="viewer-button" aria-label="关闭图片预览" @click="emit('close')">关闭</button>
        </div>
      </div>
      <div ref="stage" class="flex-1 min-h-0 overflow-auto" @click.self="emit('close')">
        <div class="min-w-full min-h-full w-max flex items-center justify-center" @click.self="emit('close')">
          <div class="relative shrink-0" :style="{ width: `${frameWidth}px`, height: `${frameHeight}px` }">
            <img :src="src" :alt="title" draggable="false" class="absolute object-contain max-w-none"
              :style="{ width: `${imageWidth}px`, height: `${imageHeight}px`, left: '50%', top: '50%', transform: `translate(-50%, -50%) rotate(${rotation}deg)` }"
              @load="onLoad" @error="failed = true" />
          </div>
        </div>
      </div>
      <p class="text-center text-xs text-white/70 pt-3 pb-[env(safe-area-inset-bottom)]">
        {{ failed ? '图片加载失败，请关闭后重试' : '放大后可滚动查看细节，点击空白处或按 Esc 关闭' }}
      </p>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { lockBodyScroll, unlockBodyScroll } from '../utils/scroll-lock'

defineProps<{ src: string; title: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLElement | null>(null)
const stage = ref<HTMLElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const zoom = ref(1)
const rotation = ref(0)
const failed = ref(false)
const natural = ref({ width: 1, height: 1 })
const available = ref({ width: 1, height: 1 })
const sideways = computed(() => rotation.value % 180 !== 0)
const fit = computed(() => Math.min(
  available.value.width / (sideways.value ? natural.value.height : natural.value.width),
  available.value.height / (sideways.value ? natural.value.width : natural.value.height),
))
const imageWidth = computed(() => natural.value.width * fit.value * zoom.value)
const imageHeight = computed(() => natural.value.height * fit.value * zoom.value)
const frameWidth = computed(() => sideways.value ? imageHeight.value : imageWidth.value)
const frameHeight = computed(() => sideways.value ? imageWidth.value : imageHeight.value)
let observer: ResizeObserver | undefined
let previousFocus: HTMLElement | null = null

function onLoad(event: Event) {
  const img = event.target as HTMLImageElement
  natural.value = { width: img.naturalWidth, height: img.naturalHeight }
  failed.value = false
}

function reset() {
  zoom.value = 1
  rotation.value = 0
  stage.value?.scrollTo(0, 0)
}

function trapFocus(event: KeyboardEvent) {
  const buttons = Array.from(dialog.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])
  const first = buttons[0]
  const last = buttons[buttons.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last?.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first?.focus()
  }
}

onMounted(() => {
  previousFocus = document.activeElement as HTMLElement | null
  lockBodyScroll()
  closeButton.value?.focus()
  observer = new ResizeObserver(() => {
    if (stage.value) available.value = { width: stage.value.clientWidth, height: stage.value.clientHeight }
  })
  if (stage.value) observer.observe(stage.value)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  unlockBodyScroll()
  if (previousFocus?.isConnected) previousFocus.focus()
})
</script>

<style scoped>
.viewer-button {
  @apply rounded-lg bg-white/15 px-3 py-2 hover:bg-white/25 focus-visible:outline focus-visible:outline-2 focus-visible:outline-white disabled:opacity-40;
}
</style>
