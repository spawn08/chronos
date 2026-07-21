import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

// Manual sidebar mirroring the original Jekyll _data/navigation.yml structure.
const sidebars: SidebarsConfig = {
  docsSidebar: [
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: [
        {type: 'link', label: 'Introduction', href: '/'},
        'getting-started/installation',
        'getting-started/cli-install',
        'getting-started/quickstart',
        'getting-started/configuration',
      ],
    },
    {
      type: 'category',
      label: 'Core Concepts',
      items: [
        'guides/real-world-agents',
        'guides/agents',
        'guides/models',
        'guides/tools',
        'guides/mcp',
        'guides/memory',
        'guides/context-management',
        'guides/teams',
        'guides/stategraph',
      ],
    },
    {
      type: 'category',
      label: 'Middleware',
      items: [
        'guides/middleware',
        'guides/hooks',
        'guides/guardrails',
        'guides/cost-tracking',
      ],
    },
    {
      type: 'category',
      label: 'Infrastructure',
      items: ['guides/storage', 'guides/streaming', 'guides/durable-execution'],
    },
    {
      type: 'category',
      label: 'Examples',
      items: [
        'guides/examples',
        'guides/examples/fundamentals',
        'guides/examples/llm-agents',
        'guides/examples/providers',
        'guides/examples/durability',
        'guides/examples/enterprise',
        'guides/examples/observability-cli',
        'guides/yaml-examples',
      ],
    },
    {
      type: 'category',
      label: 'Deployment',
      items: [
        'guides/scaling-best-practices',
        'deployment/docker',
        'deployment/kubernetes',
        'deployment/cicd',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/architecture',
        'reference/providers',
        'reference/storage',
      ],
    },
    {
      type: 'category',
      label: 'API Reference',
      items: ['api/interfaces', 'api/agent-builder', 'api/cli'],
    },
  ],
};

export default sidebars;
