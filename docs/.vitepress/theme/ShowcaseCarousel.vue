<script setup lang="ts">
// 走马灯（对齐 atlas 的 ShowcaseCarousel 版式）+ 灯箱大图：
// 自动播放 + 箭头 + 圆点 + 键盘/触摸 + 悬停暂停 + 点击看大图（Esc/再点关闭）
// + prefers-reduced-motion + SSG 安全（onMounted 才启动定时器）+ aria 完整语义。
import { ref, onMounted, onBeforeUnmount, computed, watch } from 'vue'
import { withBase } from 'vitepress'

const props = withDefaults(
  defineProps<{
    slides: { image: string; title: string; alt?: string; caption?: string }[]
    /** 自动播放间隔（毫秒）；0 = 不自动播放 */
    interval?: number
  }>(),
  { interval: 4500 },
)

const index = ref(0)
const timer = ref<ReturnType<typeof setInterval> | null>(null)
const reducedMotion = ref(false)
const paused = ref(false)
const lightbox = ref(false)

const total = computed(() => props.slides.length)

function go(i: number) {
  // total 是 computed ref：script 代码里必须 .value（模板里才自动解包）
  index.value = ((i % total.value) + total.value) % total.value
}
const next = () => go(index.value + 1)
const prev = () => go(index.value - 1)

function start() {
  stop()
  if (props.interval > 0 && !reducedMotion.value && total.value > 1) {
    timer.value = setInterval(() => {
      if (!paused.value) next()
    }, props.interval)
  }
}
function stop() {
  if (timer.value) {
    clearInterval(timer.value)
    timer.value = null
  }
}

let touchX = 0
function onTouchStart(e: TouchEvent) {
  touchX = e.touches[0].clientX
}
function onTouchEnd(e: TouchEvent) {
  const dx = e.changedTouches[0].clientX - touchX
  if (Math.abs(dx) > 40) (dx < 0 ? next : prev)()
}

// 灯箱：打开即停轮播，Esc/点击/关闭按钮退出；开着时 ←/→ 仍可翻页
function openLightbox() {
  lightbox.value = true
}
function closeLightbox() {
  lightbox.value = false
}
function onKeydown(e: KeyboardEvent) {
  if (!lightbox.value) return
  if (e.key === 'Escape') closeLightbox()
  else if (e.key === 'ArrowLeft') prev()
  else if (e.key === 'ArrowRight') next()
}
watch(lightbox, (open) => {
  if (open) stop()
  else start()
  if (typeof document !== 'undefined') document.documentElement.style.overflow = open ? 'hidden' : ''
})

onMounted(() => {
  reducedMotion.value = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  document.addEventListener('keydown', onKeydown)
  start()
})
onBeforeUnmount(() => {
  stop()
  document.removeEventListener('keydown', onKeydown)
  document.documentElement.style.overflow = ''
})

defineExpose({ go, next, prev })
</script>

<template>
  <div
    class="sc"
    role="region"
    :aria-roledescription="'carousel'"
    :aria-label="`界面速览，共 ${total} 张`"
    @mouseenter="paused = true"
    @mouseleave="paused = false"
    @focusin="paused = true"
    @focusout="paused = false"
    @keydown.left.prevent="prev"
    @keydown.right.prevent="next"
    @touchstart.passive="onTouchStart"
    @touchend.passive="onTouchEnd"
    tabindex="0"
  >
    <div class="sc-viewport">
      <div
        v-for="(slide, i) in slides"
        :key="slide.image"
        class="sc-slide"
        :class="{ active: i === index }"
        :aria-hidden="i !== index"
        :aria-label="`查看大图：${slide.title}`"
        :inert="i !== index"
        @click="openLightbox"
      >
        <img
          class="sc-img"
          :src="withBase(slide.image)"
          :alt="slide.alt ?? slide.title"
          :loading="i === 0 ? 'eager' : 'lazy'"
          :decoding="i === 0 ? 'sync' : 'async'"
          width="1440"
          height="900"
        />
        <span class="sc-caption" v-if="slide.caption">{{ slide.caption }}</span>
      </div>

      <button class="sc-arrow prev" type="button" :aria-label="'上一张'" @click="prev">
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path d="M15.5 4.5 8 12l7.5 7.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
      <button class="sc-arrow next" type="button" :aria-label="'下一张'" @click="next">
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path d="M8.5 4.5 16 12l-7.5 7.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>

    <div class="sc-dots" role="tablist" :aria-label="'选择图片'">
      <button
        v-for="(slide, i) in slides"
        :key="slide.image"
        type="button"
        role="tab"
        class="sc-dot"
        :class="{ active: i === index }"
        :aria-selected="i === index"
        :aria-label="slide.title"
        @click="go(i)"
      />
    </div>

    <Teleport to="body">
      <Transition name="sc-fade">
        <div
          v-if="lightbox"
          class="sc-lightbox"
          role="dialog"
          aria-modal="true"
          :aria-label="`${slides[index].title} 大图`"
          @click="closeLightbox"
        >
          <button class="sc-lb-close" type="button" aria-label="关闭大图" @click.stop="closeLightbox">
            <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
              <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>
          <figure>
            <img
              :src="withBase(slides[index].image)"
              :alt="slides[index].alt ?? slides[index].title"
              decoding="async"
            />
            <figcaption v-if="slides[index].caption">{{ slides[index].caption }}</figcaption>
          </figure>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.sc {
  outline: none;
}
.sc:focus-visible {
  border-radius: 10px;
  box-shadow: 0 0 0 2px var(--vp-c-brand-1);
}
.sc-viewport {
  position: relative;
  overflow: hidden;
  border-radius: 10px;
  border: 1px solid var(--vp-c-border);
  background: var(--vp-c-bg-alt);
  aspect-ratio: 16 / 10;
}
.sc-slide {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.45s ease;
  pointer-events: none;
}
.sc-slide.active {
  opacity: 1;
  pointer-events: auto;
  z-index: 1;
  cursor: zoom-in;
}
.sc-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.sc-caption {
  position: absolute;
  left: 12px;
  bottom: 12px;
  z-index: 2;
  padding: 4px 12px;
  border-radius: 999px;
  font-size: 13px;
  color: var(--vp-c-bg);
  background: color-mix(in srgb, var(--vp-c-text-1) 72%, transparent);
  backdrop-filter: blur(4px);
}
.sc-arrow {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  z-index: 3;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 1px solid var(--vp-c-border);
  background: color-mix(in srgb, var(--vp-c-bg) 82%, transparent);
  color: var(--vp-c-text-1);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s ease, background 0.2s ease;
}
.sc:hover .sc-arrow,
.sc:focus-within .sc-arrow,
.sc-arrow:focus-visible {
  opacity: 1;
}
.sc-arrow:hover {
  background: var(--vp-c-bg);
}
.sc-arrow.prev { left: 10px; }
.sc-arrow.next { right: 10px; }

.sc-dots {
  display: flex;
  justify-content: center;
  gap: 8px;
  margin-top: 10px;
}
.sc-dot {
  width: 9px;
  height: 9px;
  padding: 0;
  border-radius: 50%;
  border: none;
  cursor: pointer;
  background: var(--vp-c-divider);
  transition: transform 0.2s ease, background 0.2s ease;
}
.sc-dot.active {
  background: var(--vp-c-brand-1);
  transform: scale(1.25);
}

.sc-lightbox {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px;
  background: rgba(0, 0, 0, 0.9);
  cursor: zoom-out;
}
.sc-lightbox figure {
  margin: 0;
  max-width: 100%;
  text-align: center;
}
.sc-lightbox img {
  max-width: 100%;
  max-height: 84vh;
  border-radius: 8px;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.5);
}
.sc-lightbox figcaption {
  margin-top: 12px;
  font-size: 14px;
  color: rgba(255, 255, 255, 0.85);
}
.sc-lb-close {
  position: absolute;
  top: 16px;
  right: 20px;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.35);
  background: rgba(0, 0, 0, 0.4);
  color: #fff;
  cursor: pointer;
}
.sc-lb-close:hover {
  background: rgba(255, 255, 255, 0.15);
}
.sc-fade-enter-active,
.sc-fade-leave-active {
  transition: opacity 0.25s ease;
}
.sc-fade-enter-from,
.sc-fade-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .sc-slide {
    transition: none;
  }
  .sc-dot,
  .sc-arrow,
  .sc-fade-enter-active,
  .sc-fade-leave-active {
    transition: none;
  }
}
</style>
