import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import CodeBlock from '@theme/CodeBlock';
import Heading from '@theme/Heading';

import styles from './index.module.css';

type Feature = {icon: string; title: string; description: ReactNode};

const FEATURES: Feature[] = [
  {
    icon: '⚙️',
    title: 'YAML-First Config',
    description:
      'Define agents, teams, and models in .chronos/agents.yaml with environment variable expansion and defaults inheritance. Run from the CLI with zero Go code.',
  },
  {
    icon: '🤖',
    title: '14+ LLM Providers',
    description:
      'OpenAI, Anthropic, Gemini, Mistral, Ollama, Azure OpenAI, Groq, DeepSeek, and any OpenAI-compatible endpoint. Swap with one line.',
  },
  {
    icon: '👥',
    title: 'Multi-Agent Teams',
    description:
      'Sequential, parallel, router, and coordinator strategies. Define teams in YAML and run them from the CLI.',
  },
  {
    icon: '🔌',
    title: 'Durable Execution',
    description:
      'StateGraph runtime with checkpointing, interrupt nodes, and resume. Survive crashes and restart exactly where you left off.',
  },
  {
    icon: '🛡️',
    title: 'Guardrails & Hooks',
    description:
      'Input/output validation, retry, rate limiting, cost tracking, caching, and observability. All composable via middleware.',
  },
  {
    icon: '🧠',
    title: 'Memory & RAG',
    description:
      'Short-term and long-term memory with LLM-powered extraction. Vector-backed retrieval injected into agent context.',
  },
  {
    icon: '📦',
    title: '10 Storage Adapters',
    description:
      'SQLite, PostgreSQL, Redis, MongoDB, DynamoDB, Qdrant, Pinecone, Weaviate, Milvus. One interface, any backend.',
  },
  {
    icon: '💬',
    title: 'Context Summarization',
    description:
      'Automatic conversation summarization when approaching token limits. Rolling summaries preserve key facts within the context window.',
  },
  {
    icon: '🚀',
    title: 'Production Ready',
    description:
      'Docker, Helm chart with HPA and Ingress, CI/CD with GitHub Actions, cross-platform binaries.',
  },
];

const STATS = [
  {number: '14+', label: 'LLM Providers'},
  {number: '10', label: 'Storage Adapters'},
  {number: '4', label: 'Team Strategies'},
  {number: '6', label: 'Middleware Hooks'},
];

const ARCH = `┌──────────────────────────────────────────────────────────────┐
│                   ChronosOS  (Control Plane)                   │
│   Auth & RBAC  │  Tracing & Audit  │  Approval  │  HTTP API    │
└────────────────────────────┬───────────────────────────────────┘
                             │
┌────────────────────────────▼───────────────────────────────────┐
│                            Engine                                │
│  StateGraph Runtime │ Model Providers │ Tools │ Guardrails       │
│  Hooks & Middleware │ SSE Streaming                              │
└────────────────────────────┬───────────────────────────────────┘
                             │
┌────────────────────────────▼───────────────────────────────────┐
│                             SDK                                  │
│  Agent Builder │ Teams │ Protocol Bus │ Skills │ Memory/RAG      │
└────────────────────────────┬───────────────────────────────────┘
                             │
┌────────────────────────────▼───────────────────────────────────┐
│                     Storage  (Pluggable)                         │
│  SQLite │ PostgreSQL │ Redis │ MongoDB │ DynamoDB                │
│  Qdrant │ Pinecone │ Weaviate │ Milvus                           │
└──────────────────────────────────────────────────────────────────┘`;

const YAML_EXAMPLE = `# .chronos/agents.yaml
agents:
  - id: assistant
    name: Assistant
    model:
      provider: openai
      model: gpt-4o
      api_key: \${OPENAI_API_KEY}
    system_prompt: You are a helpful assistant.`;

const RUN_EXAMPLE = `export OPENAI_API_KEY=sk-...
go run ./cli/main.go run "What is the capital of France?"`;

const GO_EXAMPLE = `a, _ := agent.New("chat", "Chat Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithSystemPrompt("You are a helpful assistant.").
    Build()

resp, _ := a.Chat(ctx, "What is the capital of France?")
fmt.Println(resp.Content)`;

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className={styles.heroTitle}>
          {siteConfig.title}
        </Heading>
        <p className={styles.heroTagline}>
          A Go framework for building durable, scalable AI agents.
          <br />
          Define agents in YAML. Connect any LLM. Let them collaborate.
        </p>
        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/getting-started/quickstart">
            Get Started
          </Link>
          <Link
            className="button button--secondary button--lg"
            href="https://github.com/spawn08/chronos">
            GitHub
          </Link>
        </div>
        <div className={styles.installCommand}>
          <span className={styles.prompt}>$ </span>
          curl -fsSL
          https://raw.githubusercontent.com/spawn08/chronos/main/install.sh |
          bash
        </div>
        <div className={clsx(styles.installCommand, styles.installCommandAlt)}>
          <span className={styles.prompt}>$ </span>
          go get github.com/spawn08/chronos
        </div>
      </div>
    </header>
  );
}

function StatsBar() {
  return (
    <div className={styles.statsBar}>
      {STATS.map((s) => (
        <div key={s.label} className={styles.stat}>
          <div className={styles.statNumber}>{s.number}</div>
          <div className={styles.statLabel}>{s.label}</div>
        </div>
      ))}
    </div>
  );
}

function Features() {
  return (
    <section className={styles.section}>
      <div className="container">
        <Heading as="h2" className={styles.sectionTitle}>
          Key Features
        </Heading>
        <div className={styles.featureGrid}>
          {FEATURES.map((f) => (
            <div key={f.title} className={styles.featureCard}>
              <Heading as="h3" className={styles.featureCardTitle}>
                <span className={styles.featureIcon}>{f.icon}</span>
                {f.title}
              </Heading>
              <p>{f.description}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function Architecture() {
  return (
    <section className={clsx(styles.section, styles.sectionAlt)}>
      <div className="container">
        <Heading as="h2" className={styles.sectionTitle}>
          Architecture
        </Heading>
        <CodeBlock language="text" className={styles.archBlock}>
          {ARCH}
        </CodeBlock>
      </div>
    </section>
  );
}

function QuickStart() {
  return (
    <section className={styles.section}>
      <div className="container">
        <Heading as="h2" className={styles.sectionTitle}>
          Quick Start
        </Heading>
        <p className={styles.quickStartLead}>
          <strong>YAML approach</strong> — define an agent and run it:
        </p>
        <CodeBlock language="yaml">{YAML_EXAMPLE}</CodeBlock>
        <CodeBlock language="bash">{RUN_EXAMPLE}</CodeBlock>
        <p className={styles.quickStartLead}>
          <strong>Go builder</strong> — for programmatic control:
        </p>
        <CodeBlock language="go">{GO_EXAMPLE}</CodeBlock>
        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/getting-started/quickstart">
            Read the Quickstart
          </Link>
          <Link
            className="button button--secondary button--lg"
            to="/guides/yaml-examples">
            YAML Examples
          </Link>
          <Link
            className="button button--secondary button--lg"
            to="/guides/agents">
            Explore the Docs
          </Link>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={siteConfig.title}
      description="A Go framework for building durable, scalable AI agents.">
      <HomepageHeader />
      <main>
        <StatsBar />
        <Features />
        <Architecture />
        <QuickStart />
      </main>
    </Layout>
  );
}
