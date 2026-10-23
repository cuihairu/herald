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
        nav: [
          { text: '快速开始', link: '/zh/guide/getting-started' },
          { text: '使用场景', link: '/zh/guide/use-cases' },
          { text: '服务商', link: '/zh/providers/overview' },
          { text: '指南', link: '/zh/guide/introduction' },
          { text: '架构', link: '/zh/architecture/overview' },
          { text: '运行时', link: '/zh/runtime/overview' },
          { text: 'API', link: '/zh/api/overview' },
        ],
        sidebar: {
          '/zh/guide/': [
            {
              text: '入门',
              items: [
                { text: '简介', link: '/zh/guide/introduction' },
                { text: '快速开始', link: '/zh/guide/getting-started' },
                { text: '使用场景与集成', link: '/zh/guide/use-cases' },
                { text: '集成指南（对外项目）', link: '/zh/guide/integration' },
                { text: '事件与告警模型', link: '/zh/guide/events' },
              ],
            },
            {
              text: '配置',
              items: [
                { text: '配置参考', link: '/zh/guide/configuration' },
                { text: '模板系统', link: '/zh/guide/templates' },
              ],
            },
            {
              text: '运维',
              items: [
                { text: '部署', link: '/zh/guide/deployment' },
                { text: '故障排查指南', link: '/zh/guide/troubleshooting' },
              ],
            },
          ],
          '/zh/architecture/': [
            {
              text: '架构',
              items: [
                { text: '总览', link: '/zh/architecture/overview' },
                { text: '核心设计', link: '/zh/architecture/core-design' },
                { text: '模块设计', link: '/zh/architecture/modules' },
                { text: '协议设计', link: '/zh/architecture/protocols' },
                { text: '模板系统', link: '/zh/architecture/templates' },
                { text: '目录结构', link: '/zh/architecture/directory' },
                { text: '测试覆盖标准', link: '/zh/architecture/coverage' },
                { text: '覆盖率完成报告（2026-09）', link: '/zh/coverage-final-report-2026-09' },
                { text: '覆盖度专项总结（2026-09）', link: '/zh/coverage-initiative-2026-09' },
              ],
            },
          ],
          '/zh/runtime/': [
            {
              text: '运行时',
              items: [
                { text: '总览', link: '/zh/runtime/overview' },
                { text: '内置运行时', link: '/zh/runtime/builtin' },
                { text: 'Worker 运行时', link: '/zh/runtime/worker' },
                { text: '服务商类别', link: '/zh/runtime/providers' },
                { text: 'Worker SDK', link: '/zh/runtime/sdk' },
              ],
            },
          ],
          '/zh/api/': [
            {
              text: 'API',
              items: [
                { text: '总览', link: '/zh/api/overview' },
                { text: 'REST API', link: '/zh/api/rest' },
                { text: '应用 API', link: '/zh/api/apps' },
                { text: 'OpenAPI 规范', link: '/zh/api/apps#openapi-specification' },
              ],
            },
          ],
          '/zh/': [
            {
              text: '服务商指南',
              items: [
                { text: '总览与选择', link: '/zh/providers/overview' },
                { text: '短信服务商对比', link: '/zh/providers/sms' },
              ],
            },
            {
              text: '消息渠道',
              items: [
                { text: 'Telegram', link: '/zh/providers/telegram' },
                { text: '飞书', link: '/zh/providers/feishu' },
                { text: '企业微信', link: '/zh/providers/wecom' },
                { text: '钉钉', link: '/zh/providers/dingtalk' },
                { text: 'Slack', link: '/zh/providers/slack' },
                { text: 'Discord', link: '/zh/providers/discord' },
                { text: '微信个人号推送', link: '/zh/providers/wechat' },
                { text: '微信公众号指南', link: '/zh/providers/wechat-official' },
                { text: '微信公众号模板消息', link: '/zh/providers/wechatmp' },
              ],
            },
            {
              text: '短信与邮箱',
              items: [
                { text: '阿里云短信', link: '/zh/providers/aliyunsms' },
                { text: '腾讯云短信', link: '/zh/providers/tencentsms' },
                { text: '网易短信', link: '/zh/providers/neteasesms' },
                { text: '邮箱', link: '/zh/providers/email' },
              ],
            },
            {
              text: '应用推送',
              items: [
                { text: 'FCM', link: '/zh/providers/fcm' },
                { text: 'APNs', link: '/zh/providers/apns' },
                { text: 'JPush', link: '/zh/providers/jpush' },
                { text: '个推', link: '/zh/providers/getui' },
              ],
            },
            {
              text: '集成与调试',
              items: [
                { text: 'Webhook', link: '/zh/providers/webhook' },
                { text: '日志', link: '/zh/providers/log' },
              ],
            },
            {
              text: '设计文档',
              items: [
                { text: '以 Go 库方式使用 Herald', link: '/zh/library-usage' },
                { text: '通知规则引擎', link: '/zh/rule-engine-design' },
                { text: '规则引擎决策层', link: '/zh/design-rule-engine' },
                { text: '通知分组', link: '/zh/design-notification-groups' },
                { text: '受众领域模型（总览）', link: '/zh/design-audience-model' },
                { text: '受众订阅与投递枢纽（关系详解）', link: '/zh/design-audience-relations' },
                { text: '概念边界与分层审计', link: '/zh/design-audience-boundaries' },
                { text: '受众重构审计（存档）', link: '/zh/design-audience-audit' },
                { text: '冻结批次解冻计划（待决策）', link: '/zh/design-audience-deferrals' },
              ],
            },
          ],
        },
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
