import {useState} from 'react';
import styles from './styles.module.css';

type Flow = {id: string; label: string; description: string; steps: {title: string; detail: string; tone?: string}[]};

const layers = [
  {name: 'ChronosOS', path: 'os/', role: 'Control plane', tone: 'os', items: ['Auth & RBAC', 'Approvals', 'Tracing', 'HTTP API']},
  {name: 'SDK', path: 'sdk/', role: 'Application API', tone: 'sdk', items: ['Agents', 'Teams', 'Memory', 'Knowledge', 'Skills', 'Protocol']},
  {name: 'Engine', path: 'engine/', role: 'Runtime primitives', tone: 'engine', items: ['StateGraph', 'Models', 'Tools', 'Guardrails', 'Hooks', 'Queue']},
  {name: 'Storage', path: 'storage/', role: 'Persistence seams', tone: 'storage', items: ['Storage', 'VectorStore', 'Adapters', 'Migrations']},
];

const flows: Flow[] = [
  {id: 'chat', label: 'Agent chat', description: 'A single request moves through policy, model execution, tools, and durable audit storage.', steps: [
    {title: 'Request', detail: 'Agent receives the user message and assembles system context, memory, and history.', tone: 'violet'},
    {title: 'Protect', detail: 'Guardrails validate input; hooks apply logging, rate limits, cache, retry, and cost tracking.', tone: 'blue'},
    {title: 'Reason', detail: 'The selected model provider produces a response or requests one or more tools.', tone: 'violet'},
    {title: 'Act', detail: 'The registry checks permissions, executes approved tools, and feeds results back to the model.', tone: 'amber'},
    {title: 'Persist', detail: 'Output is checked, then events, usage, and trace data are appended to storage.', tone: 'green'},
  ]},
  {id: 'durability', label: 'Durable run', description: 'StateGraph records progress after every completed node so any worker can resume safely.', steps: [
    {title: 'Start run', detail: 'A worker loads the initial state and selects the first graph node.', tone: 'violet'},
    {title: 'Execute node', detail: 'The node returns new state or pauses at an interrupt for human review.', tone: 'blue'},
    {title: 'Checkpoint', detail: 'The checkpoint and event are committed together in one transaction.', tone: 'amber'},
    {title: 'Resume', detail: 'After a crash, lease handoff, or approval, a worker reads the latest sequence number.', tone: 'violet'},
    {title: 'Continue once', detail: 'Completed work is not replayed; only the next eligible node executes.', tone: 'green'},
  ]},
  {id: 'mcp', label: 'MCP tools', description: 'The MCP client imports remote tools into the same local registry used by an agent.', steps: [
    {title: 'Connect', detail: 'The client opens a stdio or HTTP+SSE transport and initializes the server.', tone: 'violet'},
    {title: 'Discover', detail: 'Advertised tools are listed and registered with typed definitions.', tone: 'blue'},
    {title: 'Call', detail: 'An agent requests a tool through the local registry and policy checks.', tone: 'amber'},
    {title: 'Proxy', detail: 'The client sends JSON-RPC and correlates the server response.', tone: 'violet'},
    {title: 'Respond', detail: 'The result is returned to the model as a regular tool result.', tone: 'green'},
  ]},
];

export default function ArchitectureExplorer() {
  const [flowID, setFlowID] = useState('chat');
  const flow = flows.find((item) => item.id === flowID) ?? flows[0];

  return <div className={styles.explorer}>
    <section className={styles.hero}>
      <div>
        <span className={styles.kicker}>System design</span>
        <h1>One architecture. Clear seams.</h1>
        <p>Chronos separates product code, durable runtime concerns, and infrastructure adapters so each can evolve independently.</p>
      </div>
      <div className={styles.rules} aria-label="Architecture rules">
        <div><strong>↓</strong><span>Dependencies flow downward</span></div>
        <div><strong>0</strong><span>Import cycles by design</span></div>
        <div><strong>∞</strong><span>Adapters behind interfaces</span></div>
      </div>
    </section>

    <section className={styles.section}>
      <header><span className={styles.kicker}>01 · Layer stack</span><h2>Build at the top. Depend only below.</h2><p>The package boundary is intentionally simple: higher layers orchestrate; lower layers provide reusable capability.</p></header>
      <div className={styles.stack}>
        {layers.map((layer, index) => <div key={layer.name} className={`${styles.layer} ${styles[layer.tone]}`}>
          <div className={styles.layerHead}><span className={styles.layerIndex}>0{index + 1}</span><div><strong>{layer.name}</strong><code>{layer.path}</code></div><span>{layer.role}</span></div>
          <div className={styles.pills}>{layer.items.map((item) => <span key={item}>{item}</span>)}</div>
        </div>)}
      </div>
      <p className={styles.note}>Only downward imports are permitted: <code>os/ → sdk/ → engine/ → storage/</code>. <code>sandbox/</code> is an isolated capability used by the tool runtime.</p>
    </section>

    <section className={styles.section}>
      <header><span className={styles.kicker}>02 · Runtime flows</span><h2>Follow the work, not an arrow maze.</h2><p>Choose a common path to see the responsibility handoff at each stage.</p></header>
      <div className={styles.flowTabs} role="tablist" aria-label="Runtime flows">
        {flows.map((item) => <button key={item.id} className={item.id === flow.id ? styles.active : ''} onClick={() => setFlowID(item.id)} role="tab" aria-selected={item.id === flow.id}>{item.label}</button>)}
      </div>
      <div className={styles.flowPanel} role="tabpanel">
        <p className={styles.flowDescription}>{flow.description}</p>
        <ol className={styles.flowSteps}>
          {flow.steps.map((step, index) => <li key={step.title} className={styles[step.tone ?? 'violet']}><span>{String(index + 1).padStart(2, '0')}</span><div><strong>{step.title}</strong><p>{step.detail}</p></div></li>)}
        </ol>
      </div>
    </section>

    <section className={styles.splitSection}>
      <div>
        <span className={styles.kicker}>03 · StateGraph</span><h2>Durability is part of the execution path.</h2><p>Every completed node produces a checkpoint. Interrupts become first-class, resumable states rather than exceptional control flow.</p>
        <ul className={styles.checklist}><li>Ordered checkpoints use a sequence number, not wall-clock time.</li><li>Checkpoint and event persistence share a transaction.</li><li>Resuming skips only the already-completed interrupt node.</li></ul>
      </div>
      <div className={styles.stateMachine} aria-label="StateGraph execution path">
        <div className={styles.stateNode}>Start</div><i /><div className={styles.stateNode}>Plan</div><i /><div className={styles.stateNode}>Tool</div>
        <div className={styles.branch}><span>needs review</span><i /><div className={`${styles.stateNode} ${styles.pause}`}>Paused</div><i /><div className={styles.stateNode}>Resume</div></div>
        <div className={styles.checkpoint}>✓ checkpoint after every completed node</div>
      </div>
    </section>

    <section className={styles.section}>
      <header><span className={styles.kicker}>04 · Extension points</span><h2>Small interfaces, replaceable infrastructure.</h2></header>
      <div className={styles.interfaceGrid}>
        <article><span className={styles.interfaceTag}>Persistence</span><h3>Storage</h3><p>Sessions, memory, audit logs, traces, events, and checkpoints.</p><code>storage.Storage</code></article>
        <article><span className={styles.interfaceTag}>Retrieval</span><h3>VectorStore</h3><p>Collection management, vector upsert, search, and deletion.</p><code>storage.VectorStore</code></article>
        <article><span className={styles.interfaceTag}>Intelligence</span><h3>Provider</h3><p>Chat and streaming implementations for any model backend.</p><code>model.Provider</code></article>
        <article><span className={styles.interfaceTag}>Safety</span><h3>Guardrail & Hook</h3><p>Composable checks and before/after middleware around execution.</p><code>guardrails.Guardrail</code></article>
        <article><span className={styles.interfaceTag}>Isolation</span><h3>Sandbox</h3><p>Process, container, WASM, or Kubernetes execution for untrusted code.</p><code>sandbox.Sandbox</code></article>
        <article><span className={styles.interfaceTag}>Coordination</span><h3>Team strategy</h3><p>Sequential, parallel, router, coordinator, swarm, and hierarchy topologies.</p><code>sdk/team</code></article>
      </div>
    </section>

    <section className={styles.deploySection}>
      <div><span className={styles.kicker}>05 · Deployment</span><h2>Same application API, from laptop to fleet.</h2></div>
      <div className={styles.deployGrid}>
        <article><span className={styles.deployLabel}>Development</span><h3>One binary</h3><p>Chronos with SQLite provides a small, local feedback loop.</p><div className={styles.deployDiagram}><b>chronos</b><i>→</i><b>SQLite</b></div></article>
        <article><span className={styles.deployLabel}>Production</span><h3>Durable workers</h3><p>Replicas coordinate through PostgreSQL leases and a shared event store.</p><div className={styles.deployDiagram}><b>Ingress</b><i>→</i><b>Workers × N</b><i>→</i><b>Postgres</b></div></article>
        <article><span className={styles.deployLabel}>Scale</span><h3>Kubernetes</h3><p>Use deployment replicas, HPA, disruption budgets, and observability.</p><div className={styles.deployDiagram}><b>HPA</b><i>→</i><b>Deployment</b><i>→</i><b>Service</b></div></article>
      </div>
    </section>
  </div>;
}
