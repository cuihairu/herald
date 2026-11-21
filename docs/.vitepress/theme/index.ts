// herald docs theme — 默认主题 + 品牌配色（custom.css 见同目录）
// VitePress 会自动发现本文件，无需在 config.mts 里引用。
import { onMounted } from 'vue'
import { useData } from 'vitepress/client'
import DefaultTheme from 'vitepress/theme'
import ShowcaseCarousel from './ShowcaseCarousel.vue'
import { applyAutoLocale } from './lang'
import './custom.css'
import type { Theme } from 'vitepress'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('ShowcaseCarousel', ShowcaseCarousel)
  },
  setup() {
    // 首访语言探测：中文浏览器首次到访跳 /zh/，之后以手动选择为准（lang.ts）。
    onMounted(() => applyAutoLocale(useData().site.value.base))
  },
} satisfies Theme
