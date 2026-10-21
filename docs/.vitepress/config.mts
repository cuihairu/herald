import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Herald',
  description: 'Unified subscription and delivery hub',
  base: '/herald/',

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/herald/logo.svg' }],
  ],

  transformHtml: (html, id) => {
    if (!id.includes('/zh/')) return html
    return html.replace(
      /href="\/herald\/审计-深度审核-2026-10-19\.html"/g,
      'href="/herald/zh/"',
    )
  },

  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Quick Start', link: '/guide/getting-started' },
      { text: 'Use Cases', link: '/guide/use-cases' },
      { text: 'Providers', link: '/providers/overview' },
      { text: 'Guide', link: '/guide/introduction' },
      { text: 'Architecture', link: '/architecture/overview' },
      { text: 'Runtime', link: '/runtime/overview' },
      { text: 'API', link: '/api/overview' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Get Started',
          items: [
            { text: 'Introduction', link: '/guide/introduction' },
            { text: 'Quick Start', link: '/guide/getting-started' },
            { text: 'Use Cases and Integration', link: '/guide/use-cases' },
            { text: 'Integration Guide (For External Projects)', link: '/guide/integration' },
            { text: 'Events and Alert Model', link: '/guide/events' },
          ],
        },
        {
          text: 'Configuration',
          items: [
            { text: 'Configuration Reference', link: '/guide/configuration' },
            { text: 'Template System', link: '/guide/templates' },
          ],
        },
        {
          text: 'Operations',
          items: [
            { text: 'Deployment', link: '/guide/deployment' },
            { text: 'Troubleshooting Guide', link: '/guide/troubleshooting' },
          ],
        },
      ],
      '/architecture/': [
        {
          text: 'Architecture',
          items: [
            { text: 'Overview', link: '/architecture/overview' },
            { text: 'Core Design', link: '/architecture/core-design' },
            { text: 'Module Design', link: '/architecture/modules' },
            { text: 'Protocol Design', link: '/architecture/protocols' },
            { text: 'Template System', link: '/architecture/templates' },
            { text: 'Directory Structure', link: '/architecture/directory' },
            { text: 'Test Coverage Standards', link: '/architecture/coverage' },
            { text: 'Coverage Completion Report', link: '/coverage-final-report-2026-09' },
            { text: 'Coverage Initiative Summary (2026-09)', link: '/coverage-initiative-2026-09' },
          ],
        },
      ],
      '/runtime/': [
        {
          text: 'Runtime',
          items: [
            { text: 'Overview', link: '/runtime/overview' },
            { text: 'Builtin Runtime', link: '/runtime/builtin' },
            { text: 'Worker Runtime', link: '/runtime/worker' },
            { text: 'Provider Categories', link: '/runtime/providers' },
            { text: 'Worker SDK', link: '/runtime/sdk' },
          ],
        },
      ],
      '/api/': [
        {
          text: 'API',
          items: [
            { text: 'Overview', link: '/api/overview' },
            { text: 'REST API', link: '/api/rest' },
            { text: 'Application API', link: '/api/apps' },
            { text: 'OpenAPI Specification', link: '/api/apps#openapi-specification' },
          ],
        },
      ],
      '/': [
        {
          text: 'Provider Guides',
          items: [
            { text: 'Overview and Selection', link: '/providers/overview' },
            { text: 'SMS Provider Comparison', link: '/providers/sms' },
          ],
        },
        {
          text: 'Messaging',
          items: [
            { text: 'Telegram', link: '/providers/telegram' },
            { text: 'Feishu', link: '/providers/feishu' },
            { text: 'WeCom', link: '/providers/wecom' },
            { text: 'DingTalk', link: '/providers/dingtalk' },
            { text: 'Slack', link: '/providers/slack' },
            { text: 'Discord', link: '/providers/discord' },
            { text: 'WeChat Personal Push', link: '/providers/wechat' },
            { text: 'WeChat Official Account Guide', link: '/providers/wechat-official' },
            { text: 'WeChat Official Account Template Messages', link: '/providers/wechatmp' },
          ],
        },
        {
          text: 'SMS and Email',
          items: [
            { text: 'Alibaba Cloud SMS', link: '/providers/aliyunsms' },
            { text: 'Tencent Cloud SMS', link: '/providers/tencentsms' },
            { text: 'NetEase SMS', link: '/providers/neteasesms' },
            { text: 'Email', link: '/providers/email' },
          ],
        },
        {
          text: 'App Push',
          items: [
            { text: 'FCM', link: '/providers/fcm' },
            { text: 'APNs', link: '/providers/apns' },
            { text: 'JPush', link: '/providers/jpush' },
            { text: 'Getui', link: '/providers/getui' },
          ],
        },
        {
          text: 'Integration and Debugging',
          items: [
            { text: 'Webhook', link: '/providers/webhook' },
            { text: 'Log', link: '/providers/log' },
          ],
        },
        {
          text: 'Design Documents',
          items: [
            { text: 'Using Herald as a Go Library', link: '/library-usage' },
            { text: 'Notification Rule Engine', link: '/rule-engine-design' },
            { text: 'Rule Engine Decision Layer', link: '/design-rule-engine' },
            { text: 'Notification Groups', link: '/design-notification-groups' },
            { text: 'Audience Domain Model (Overview)', link: '/design-audience-model' },
            { text: 'Audience Subscription and Delivery Hub (Detailed Relationships)', link: '/design-audience-relations' },
            { text: 'Concept Boundaries and Layering Audit', link: '/design-audience-boundaries' },
            { text: 'Audience Refactoring Audit (Archive)', link: '/design-audience-audit' },
            { text: 'Unfreezing Plan for Held Batches (Pending Decision)', link: '/design-audience-deferrals' },
          ],
        },
      ],
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/cuihairu/herald' }],
    search: { provider: 'local' },
  },

  locales: {
    root: {
      label: 'English',
      lang: 'en',
      themeConfig: {
        outline: [2, 3],
        docFooter: { prev: 'Previous', next: 'Next' },
        lastUpdated: {
          text: 'Last updated',
          formatOptions: { dateStyle: 'medium', timeStyle: 'medium' },
        },
      },
    },
    zh: {
      label: '简体中文',
      lang: 'zh-CN',
      themeConfig: {
        outline: [2, 3],
        docFooter: { prev: '上一页', next: '下一页' },
        lastUpdated: {
          text: '最后更新于',
          formatOptions: { dateStyle: 'medium', timeStyle: 'medium' },
        },
      },
    },
  },
})
