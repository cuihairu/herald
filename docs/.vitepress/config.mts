import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Herald',
  description: '统一订阅与投递中枢',
  lang: 'zh-CN',
  base: '/herald/',

  head: [
    // head 里的资源不会自动附加 base，需与下方 base 保持一致
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/herald/logo.svg' }],
  ],

  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: '快速开始', link: '/guide/getting-started' },
      { text: '使用场景', link: '/guide/use-cases' },
      { text: 'Providers', link: '/providers/overview' },
      { text: '指南', link: '/guide/introduction' },
      { text: '架构', link: '/architecture/overview' },
      { text: 'Runtime', link: '/runtime/overview' },
      { text: 'API', link: '/api/overview' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: '开始',
          items: [
            { text: '简介', link: '/guide/introduction' },
            { text: '快速开始', link: '/guide/getting-started' },
            { text: '使用场景与接入', link: '/guide/use-cases' },
            { text: '应用接入（集成者 API）', link: '/guide/integration' },
            { text: 'ferry 对接验证（阶段③）', link: '/guide/ferry-integration' },
          ]
        },
        {
          text: '配置',
          items: [
            { text: '配置参考', link: '/guide/configuration' },
            { text: '模板系统', link: '/guide/templates' },
          ]
        },
        {
          text: '运维',
          items: [
            { text: '部署', link: '/guide/deployment' },
            { text: '排错指南', link: '/guide/troubleshooting' },
          ]
        }
      ],
      '/architecture/': [
        {
          text: '架构设计',
          items: [
            { text: '概述', link: '/architecture/overview' },
            { text: '核心设计', link: '/architecture/core-design' },
            { text: '模块设计', link: '/architecture/modules' },
            { text: '协议设计', link: '/architecture/protocols' },
            { text: '模板系统', link: '/architecture/templates' },
            { text: '目录结构', link: '/architecture/directory' },
            { text: '测试覆盖率口径', link: '/architecture/coverage' },
            { text: '覆盖率收官报告', link: '/coverage-final-report-2026-09' },
          ]
        }
      ],
      '/runtime/': [
        {
          text: 'Runtime',
          items: [
            { text: '概述', link: '/runtime/overview' },
            { text: 'Builtin Runtime', link: '/runtime/builtin' },
            { text: 'Worker Runtime', link: '/runtime/worker' },
            { text: 'Provider 分类', link: '/runtime/providers' },
            { text: 'Worker SDK', link: '/runtime/sdk' },
          ]
        }
      ],
      '/api/': [
        {
          text: 'API',
          items: [
            { text: '概述', link: '/api/overview' },
            { text: 'REST API', link: '/api/rest' },
          ]
        }
      ],
      '/': [
        {
          text: 'Provider 手册',
          items: [
            { text: '总览与选型', link: '/providers/overview' },
            { text: '三家短信对比', link: '/providers/sms' },
          ]
        },
        {
          text: '即时通讯',
          items: [
            { text: 'Telegram', link: '/providers/telegram' },
            { text: '飞书', link: '/providers/feishu' },
            { text: '企业微信', link: '/providers/wecom' },
            { text: '钉钉', link: '/providers/dingtalk' },
            { text: 'Slack', link: '/providers/slack' },
            { text: 'Discord', link: '/providers/discord' },
            { text: '微信个人推送', link: '/providers/wechat' },
            { text: '微信公众号指南', link: '/providers/wechat-official' },
            { text: '微信公众号模板消息', link: '/providers/wechatmp' },
          ]
        },
        {
          text: '短信与邮件',
          items: [
            { text: '阿里云短信', link: '/providers/aliyunsms' },
            { text: '腾讯云短信', link: '/providers/tencentsms' },
            { text: '网易云信短信', link: '/providers/neteasesms' },
            { text: 'Email', link: '/providers/email' },
          ]
        },
        {
          text: 'App 推送',
          items: [
            { text: 'FCM', link: '/providers/fcm' },
            { text: 'APNs', link: '/providers/apns' },
            { text: '极光 JPush', link: '/providers/jpush' },
            { text: '个推 Getui', link: '/providers/getui' },
          ]
        },
        {
          text: '对接与调试',
          items: [
            { text: 'Webhook', link: '/providers/webhook' },
            { text: 'Log', link: '/providers/log' },
          ]
        },
        {
          text: '设计文档',
          items: [
            { text: '作为 Go 库使用', link: '/library-usage' },
            { text: '通知规则引擎', link: '/rule-engine-design' },
            { text: '规则引擎决策层', link: '/design-rule-engine' },
            { text: '通知群组', link: '/design-notification-groups' },
            { text: '受众领域模型（总纲）', link: '/design-audience-model' },
            { text: '受众订阅与投递中枢（关系详设）', link: '/design-audience-relations' },
            { text: '概念边界与分层审计', link: '/design-audience-boundaries' },
            { text: 'Audience 改造审计（存档）', link: '/design-audience-audit' },
          ]
        }
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/cuihairu/herald' }
    ],

    search: {
      provider: 'local'
    }
  }
})
