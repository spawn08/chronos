import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'Chronos',
  tagline: 'A Go framework for building durable, scalable AI agents',
  favicon: 'img/favicon.ico',
  headTags: [
    {
      tagName: 'link',
      attributes: {
        rel: 'icon',
        type: 'image/svg+xml',
        href: '/chronos/img/logo.svg',
      },
    },
  ],

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: true, // Improve compatibility with the upcoming Docusaurus v4
  },

  // Set the production url of your site here
  url: 'https://spawn08.github.io',
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: '/chronos/',

  // GitHub pages deployment config.
  organizationName: 'spawn08', // GitHub org/user name.
  projectName: 'chronos', // Repo name.

  onBrokenLinks: 'throw',

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang.
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  // Ported docs are authored as CommonMark (.md). Use "detect" so .md files are
  // parsed as CommonMark (tolerant of angle brackets / braces in Go & YAML) while
  // .mdx files still get full MDX.
  markdown: {
    format: 'detect',
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      {
        docs: {
          // Serve docs at the site root to match the original permalinks
          // (e.g. /getting-started/quickstart).
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl:
            'https://github.com/spawn08/chronos/tree/main/website/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Chronos',
      logo: {
        alt: 'Chronos Logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/getting-started/quickstart',
          label: 'Quickstart',
          position: 'left',
        },
        {
          to: '/reference/architecture',
          label: 'Architecture',
          position: 'left',
        },
        {
          href: 'https://github.com/spawn08/chronos',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Quickstart', to: '/getting-started/quickstart'},
            {label: 'Agents', to: '/guides/agents'},
            {label: 'Multi-Agent Teams', to: '/guides/teams'},
            {label: 'Deployment', to: '/deployment/docker'},
          ],
        },
        {
          title: 'Reference',
          items: [
            {label: 'Architecture', to: '/reference/architecture'},
            {label: 'Interfaces', to: '/api/interfaces'},
            {label: 'CLI Reference', to: '/api/cli'},
          ],
        },
        {
          title: 'More',
          items: [
            {label: 'GitHub', href: 'https://github.com/spawn08/chronos'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Chronos. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['go', 'bash', 'yaml', 'json', 'docker'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
