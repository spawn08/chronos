import type {CSSProperties, ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import CodeBlock from '@theme/CodeBlock';
import Heading from '@theme/Heading';

import styles from './index.module.css';

type Pillar = {
  eyebrow: string;
  title: string;
  description: string;
  href: string;
};

type Capability = {
  title: string;
  description: string;
  href: string;
};

const PILLARS: Pillar[] = [
  {
    eyebrow: 'Build',
    title: 'Agent SDK',
    description:
      'Compose chat agents, deterministic StateGraphs, tools, memory, skills, and multi-agent teams from Go or YAML.',
    href: '/guides/agents',
  },
  {
    eyebrow: 'Run',
    title: 'Durable Runtime',
    description:
      'Checkpoint graph progress, resume interrupted work, stream events, and scale workers over a lease-backed queue.',
    href: '/guides/durable-execution',
  },
  {
    eyebrow: 'Govern',
    title: 'ChronosOS',
    description:
      'Operate agents with auth, approvals, tracing, audit logs, rate limits, and production deployment patterns.',
    href: '/guides/server',
  },
];

const CAPABILITIES: Capability[] = [
  {
    title: 'Provider-neutral models',
    description: 'OpenAI, Anthropic, Gemini, Mistral, Ollama, Azure OpenAI, Groq, DeepSeek, and OpenAI-compatible endpoints.',
    href: '/guides/models',
  },
  {
    title: 'Tools with policy',
    description: 'Typed tool definitions, permissions, approval gates, sandboxed execution, and audit-friendly results.',
    href: '/guides/tools',
  },
  {
    title: 'Memory and RAG',
    description: 'Short-term context, long-term memory extraction, vector-backed semantic recall, and knowledge search.',
    href: '/guides/memory',
  },
  {
    title: 'Teams and delegation',
    description: 'Sequential, parallel, router, coordinator, and swarm-style collaboration for specialized agents.',
    href: '/guides/teams',
  },
  {
    title: 'Interoperability',
    description: 'Expose or consume agents through MCP, A2A, and AG-UI without coupling your application to one ecosystem.',
    href: '/guides/mcp',
  },
  {
    title: 'Evaluation loop',
    description: 'Capture traces, create datasets, run evaluators, and gate regressions before changes reach production.',
    href: '/guides/eval-loop',
  },
];

const STATS = [
  {value: 'Go-native', label: 'typed SDK and runtime'},
  {value: 'Durable', label: 'checkpoints and resume'},
  {value: 'Pluggable', label: 'models, storage, vectors'},
  {value: 'Governed', label: 'auth, approval, audit'},
];

const YAML_EXAMPLE = `# .chronos/agents.yaml
agents:
  - id: support
    name: Support Agent
    model:
      provider: openai
      model: gpt-5.5
      api_key: \${OPENAI_API_KEY}
    system_prompt: You resolve customer issues clearly and safely.
    tools: [search_docs, create_ticket]
    memory:
      semantic_recall: true`;

const GO_EXAMPLE = `store, _ := sqlite.New("chronos.db")
store.Migrate(ctx)

a, _ := agent.New("support", "Support Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithStorage(store).
    WithSystemPrompt("Resolve customer issues clearly and safely.").
    Build()

resp, _ := a.ChatWithSession(ctx, "session-42", "Help me debug my order")
fmt.Println(resp.Content)`;

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={styles.hero}>
      <div className={clsx('container', styles.heroGrid)}>
        <div className={styles.heroCopy}>
          <div className={styles.announcement}>Durable agents for production Go systems</div>
          <Heading as="h1" className={styles.heroTitle}>
            {siteConfig.title} turns agent prototypes into reliable software.
          </Heading>
          <p className={styles.heroText}>
            Build AI agents that plan, call tools, remember context, collaborate in teams, and resume long-running work after failures.
          </p>
          <div className={styles.heroActions}>
            <Link className="button button--primary button--lg" to="/getting-started/quickstart">
              Start building
            </Link>
            <Link className="button button--secondary button--lg" to="/reference/architecture">
              View architecture
            </Link>
          </div>
          <div className={styles.commandBar} aria-label="Install Chronos">
            <span>$</span> curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
          </div>
        </div>
        <RuntimeVisual />
      </div>
    </header>
  );
}

function RuntimeVisual() {
  return (
    <div className={styles.visualCard} aria-label="Chronos runtime visualization">
      <div className={styles.visualTopline}>
        <span className={styles.liveDot} /> agent run · session-42
      </div>
      <div className={styles.pipeline}>
        {['Input', 'Guardrails', 'Model', 'Tools', 'Checkpoint', 'Stream'].map((item, index) => (
          <div key={item} className={styles.pipelineStep} style={{'--i': index} as CSSProperties}>
            <span>{index + 1}</span>
            {item}
          </div>
        ))}
      </div>
      <div className={styles.graphPanel}>
        <svg viewBox="0 0 520 250" role="img" aria-label="StateGraph with durable checkpoints">
          <defs>
            <linearGradient id="edge" x1="0" x2="1">
              <stop offset="0%" stopColor="#8b5cf6" />
              <stop offset="100%" stopColor="#22d3ee" />
            </linearGradient>
            <filter id="glow" x="-30%" y="-30%" width="160%" height="160%">
              <feGaussianBlur stdDeviation="4" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>
          <path d="M68 125 C122 42 190 42 244 125 S366 208 452 96" fill="none" stroke="url(#edge)" strokeWidth="4" strokeLinecap="round" />
          <path d="M68 125 C130 192 194 196 244 125 S344 54 452 156" fill="none" stroke="rgba(139,92,246,.22)" strokeWidth="3" strokeDasharray="8 10" />
          {[
            [68, 125, 'start'],
            [180, 70, 'plan'],
            [244, 125, 'tool'],
            [342, 182, 'review'],
            [452, 96, 'done'],
          ].map(([x, y, label]) => (
            <g key={label as string} filter="url(#glow)">
              <circle cx={x as number} cy={y as number} r="23" fill="#0f172a" stroke="url(#edge)" strokeWidth="3" />
              <text x={x as number} y={(y as number) + 46} textAnchor="middle" fill="currentColor" fontSize="13">
                {label as string}
              </text>
            </g>
          ))}
          <g opacity=".9">
            <rect x="34" y="196" width="116" height="34" rx="10" fill="rgba(34,211,238,.12)" stroke="rgba(34,211,238,.35)" />
            <text x="92" y="218" textAnchor="middle" fill="currentColor" fontSize="13">checkpoint</text>
            <rect x="370" y="196" width="116" height="34" rx="10" fill="rgba(139,92,246,.12)" stroke="rgba(139,92,246,.35)" />
            <text x="428" y="218" textAnchor="middle" fill="currentColor" fontSize="13">resume</text>
          </g>
        </svg>
      </div>
    </div>
  );
}

function StatsBar() {
  return (
    <section className={styles.statsSection} aria-label="Chronos platform qualities">
      <div className={clsx('container', styles.statsGrid)}>
        {STATS.map((stat) => (
          <div key={stat.value} className={styles.statCard}>
            <strong>{stat.value}</strong>
            <span>{stat.label}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function Pillars() {
  return (
    <section className={styles.section}>
      <div className="container">
        <div className={styles.sectionHeader}>
          <span className={styles.eyebrow}>Platform</span>
          <Heading as="h2">One stack for building, running, and governing agents</Heading>
          <p>Chronos keeps application code clean while the runtime handles durability, observability, and operational control.</p>
        </div>
        <div className={styles.pillarGrid}>
          {PILLARS.map((pillar) => (
            <Link key={pillar.title} to={pillar.href} className={styles.pillarCard}>
              <span>{pillar.eyebrow}</span>
              <Heading as="h3">{pillar.title}</Heading>
              <p>{pillar.description}</p>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

function Architecture() {
  return (
    <section className={clsx(styles.section, styles.archSection)}>
      <div className={clsx('container', styles.archGrid)}>
        <div>
          <span className={styles.eyebrow}>Architecture</span>
          <Heading as="h2">A layered runtime with explicit seams</Heading>
          <p>
            Swap model providers, storage backends, vector stores, tools, hooks, guardrails, and sandbox backends without rewriting your agents.
          </p>
          <Link className="button button--primary" to="/reference/architecture">
            Explore diagrams
          </Link>
        </div>
        <div className={styles.layerStack} aria-label="Chronos layered architecture">
          {[
            ['ChronosOS', 'Auth · approvals · tracing · HTTP API'],
            ['SDK', 'Agent builder · teams · memory · knowledge'],
            ['Engine', 'StateGraph · tools · models · streaming'],
            ['Storage', 'SQL · NoSQL · vector adapters'],
          ].map(([name, details], index) => (
            <div key={name} className={styles.layer} style={{'--i': index} as CSSProperties}>
              <strong>{name}</strong>
              <span>{details}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function Capabilities() {
  return (
    <section className={styles.section}>
      <div className="container">
        <div className={styles.sectionHeader}>
          <span className={styles.eyebrow}>Capabilities</span>
          <Heading as="h2">Everything needed beyond the first demo</Heading>
        </div>
        <div className={styles.capabilityGrid}>
          {CAPABILITIES.map((capability) => (
            <Link key={capability.title} to={capability.href} className={styles.capabilityCard}>
              <Heading as="h3">{capability.title}</Heading>
              <p>{capability.description}</p>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

function QuickStart() {
  return (
    <section className={clsx(styles.section, styles.quickstartSection)}>
      <div className={clsx('container', styles.quickstartGrid)}>
        <div>
          <span className={styles.eyebrow}>Quickstart</span>
          <Heading as="h2">Start with YAML. Drop into Go when you need control.</Heading>
          <p>
            Use declarative configuration for operations-friendly agents, then compose the same primitives directly from Go for product code.
          </p>
          <div className={styles.quickLinks}>
            <Link className="button button--primary" to="/getting-started/quickstart">
              Read quickstart
            </Link>
            <Link className="button button--secondary" to="/api/agent-builder">
              Agent API
            </Link>
          </div>
        </div>
        <div className={styles.codeShowcase}>
          <div className={styles.codeLabel}>YAML agent</div>
          <CodeBlock language="yaml">{YAML_EXAMPLE}</CodeBlock>
          <div className={styles.codeLabel}>Go builder</div>
          <CodeBlock language="go">{GO_EXAMPLE}</CodeBlock>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout title={siteConfig.title} description="Chronos is a Go framework for durable, scalable, production-ready AI agents.">
      <HomepageHeader />
      <main>
        <StatsBar />
        <Pillars />
        <Architecture />
        <Capabilities />
        <QuickStart />
      </main>
    </Layout>
  );
}
