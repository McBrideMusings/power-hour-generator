import { defineConfig } from 'vitepress'
import { markdownWriterPlugin } from 'vitepress-theme-pm/plugin'

export default defineConfig({
  title: 'Power Hour Generator Docs',
  description: 'CLI tool that orchestrates yt-dlp and ffmpeg to produce power hour video clip libraries',

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Architecture', link: '/architecture/overview' },
      { text: 'Development', link: '/development/setup' },
      { text: 'Board', link: '/board' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Guide',
          items: [
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'CLI Reference', link: '/guide/cli-reference' },
            { text: 'Configuration', link: '/guide/configuration' },
            { text: 'Overlays', link: '/guide/overlays' },
            { text: 'Collections', link: '/guide/collections' },
            { text: 'Templates', link: '/guide/templates' },
          ],
        },
      ],
      '/architecture/': [
        {
          text: 'Architecture',
          items: [
            { text: 'Overview', link: '/architecture/overview' },
            { text: 'CLI Layer', link: '/architecture/cli' },
            { text: 'Config System', link: '/architecture/config' },
            { text: 'Cache System', link: '/architecture/cache' },
            { text: 'Render Pipeline', link: '/architecture/render' },
            { text: 'CSV Loading', link: '/architecture/csv-loading' },
            { text: 'Tool Management', link: '/architecture/tools' },
            { text: 'TUI System', link: '/architecture/tui' },
          ],
        },
      ],
      '/development/': [
        {
          text: 'Development',
          items: [
            { text: 'Setup', link: '/development/setup' },
            { text: 'Testing', link: '/development/testing' },
            { text: 'Code Style', link: '/development/code-style' },
            { text: 'Troubleshooting', link: '/development/troubleshooting' },
            { text: 'Deployment', link: '/development/deployment' },
          ],
        },
      ],
    },

    search: {
      provider: 'local',
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/McBrideMusings/power-hour-generator' },
    ],

    editLink: {
      pattern: 'https://github.com/McBrideMusings/power-hour-generator/edit/main/docs/:path',
    },
  },

  vite: {
    server: {
      host: '0.0.0.0',
      port: 5193,
    },
    plugins: [markdownWriterPlugin()],
  },
})
