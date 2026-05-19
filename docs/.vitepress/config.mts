import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Herald',
  description: 'Event-driven Delivery Infrastructure',
  lang: 'zh-CN',
  base: '/herald/',

  themeConfig: {
    nav: [
      { text: '指南', link: '/guide/introduction' },
      { text: '架构', link: '/architecture/overview' },
      { text: 'Runtime', link: '/runtime/overview' },
      { text: 'API', link: '/api/overview' },
      { text: 'Providers', link: '/providers/overview' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: '指南',
          items: [
            { text: '简介', link: '/guide/introduction' },
            { text: '快速开始', link: '/guide/getting-started' },
            { text: '配置', link: '/guide/configuration' },
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
            { text: '目录结构', link: '/architecture/directory' },
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
            { text: '事件 API', link: '/api/events' },
          ]
        }
      ],
      '/': [
        {
          text: 'Providers',
          items: [
            { text: '概述', link: '/providers/overview' },
            { text: '微信个人推送', link: '/providers/wechat' },
            { text: '微信公众号', link: '/providers/wechat-official' },
            { text: 'SMS Providers', link: '/providers/sms' },
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
