// Chronos dashboard — plain JS, no build step, no external dependencies.
// Talks to the same-origin ChronosOS API: /api/sessions, /api/dashboard/*,
// /api/approval/*, and /api/agui/stream for live updates.
(() => {
  "use strict";

  const els = {
    connStatus: document.getElementById("conn-status"),
    sessionList: document.getElementById("session-list"),
    emptyState: document.getElementById("empty-state"),
    runView: document.getElementById("run-view"),
    runSessionId: document.getElementById("run-session-id"),
    runStatus: document.getElementById("run-status"),
    resumeBtn: document.getElementById("resume-btn"),
    graphSvg: document.getElementById("graph-svg"),
    costView: document.getElementById("cost-view"),
    approvalList: document.getElementById("approval-list"),
    checkpointList: document.getElementById("checkpoint-list"),
    stateView: document.getElementById("state-view"),
    eventLog: document.getElementById("event-log"),
  };

  let selected = null; // currently selected session id
  let stream = null; // active EventSource
  let sessionsByID = new Map();

  // Auth token support: when the control plane has JWT/API-key auth enabled,
  // every fetch() call needs it. EventSource cannot set custom headers (a
  // browser limitation), so the live /api/agui/stream connection only works
  // unauthenticated or behind a proxy that injects the header — see
  // website/docs/guides/dashboard.md.
  const AUTH_KEY = "chronos_dashboard_token";
  const tokenInput = document.getElementById("auth-token");
  tokenInput.value = localStorage.getItem(AUTH_KEY) || "";
  document.getElementById("auth-save").onclick = () => {
    localStorage.setItem(AUTH_KEY, tokenInput.value);
  };

  function authHeaders() {
    const token = localStorage.getItem(AUTH_KEY);
    if (!token) return {};
    return token.split(".").length === 3
      ? { Authorization: `Bearer ${token}` } // looks like a JWT
      : { "X-Api-Key": token };
  }

  async function fetchJSON(url, opts = {}) {
    const headers = Object.assign({}, opts.headers, authHeaders());
    const res = await fetch(url, Object.assign({}, opts, { headers }));
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || `${url}: HTTP ${res.status}`);
    return body;
  }

  function badge(el, status) {
    el.textContent = status;
    el.className = "badge " + status;
  }

  // ---- sessions list -------------------------------------------------

  async function loadSessions() {
    const data = await fetchJSON("/api/sessions");
    const sessions = data.sessions || [];
    sessionsByID = new Map(sessions.map((s) => [s.id, s]));
    els.sessionList.innerHTML = "";
    for (const s of sessions) {
      const li = document.createElement("li");
      const idSpan = document.createElement("span");
      idSpan.textContent = s.id;
      const agentSpan = document.createElement("span");
      agentSpan.className = "agent";
      agentSpan.textContent = s.agent_id;
      li.appendChild(idSpan);
      li.appendChild(agentSpan);
      li.className = s.id === selected ? "selected" : "";
      li.onclick = () => selectSession(s.id);
      els.sessionList.appendChild(li);
    }
    if (selected && sessionsByID.has(selected)) {
      badge(els.runStatus, sessionsByID.get(selected).status);
      els.resumeBtn.hidden = sessionsByID.get(selected).status !== "paused";
    }
  }

  // ---- graph rendering (simple BFS-layered SVG, no external library) --

  function renderGraph(view, activeNode) {
    const svg = els.graphSvg;
    svg.innerHTML = `<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
      <path d="M0,0 L0,6 L7,3 z" fill="#9aa4b2" /></marker></defs>`;
    if (!view || !view.nodes || view.nodes.length === 0) return;

    // Layer nodes by BFS distance from "__start__" over the edge list.
    const children = new Map();
    for (const e of view.edges) {
      if (!e.to) continue; // unresolved conditional edge: no fixed target to lay out
      if (!children.has(e.from)) children.set(e.from, []);
      children.get(e.from).push(e.to);
    }
    const depth = new Map([["__start__", 0]]);
    const queue = ["__start__"];
    while (queue.length) {
      const n = queue.shift();
      for (const c of children.get(n) || []) {
        if (!depth.has(c)) {
          depth.set(c, depth.get(n) + 1);
          queue.push(c);
        }
      }
    }
    const maxDepth = Math.max(0, ...[...depth.values()]);
    const byDepth = new Map();
    for (const node of view.nodes) {
      const d = depth.has(node.id) ? depth.get(node.id) : maxDepth + 1; // unreachable: park at the end
      if (!byDepth.has(d)) byDepth.set(d, []);
      byDepth.get(d).push(node);
    }

    const width = svg.clientWidth || 600;
    const rowH = 70;
    const nodeW = 120;
    const nodeH = 34;
    const positions = new Map();
    const rows = [...byDepth.keys()].sort((a, b) => a - b);
    for (const d of rows) {
      const nodes = byDepth.get(d);
      const gap = width / (nodes.length + 1);
      nodes.forEach((n, i) => {
        positions.set(n.id, { x: gap * (i + 1), y: d * rowH + 30 });
      });
    }
    svg.setAttribute("height", (rows.length * rowH + 40) + "");

    // Edges first, so nodes draw on top.
    for (const e of view.edges) {
      if (!e.to) continue;
      const a = positions.get(e.from), b = positions.get(e.to);
      if (!a || !b) continue;
      const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
      path.setAttribute("class", "gedge");
      path.setAttribute("d", `M${a.x},${a.y + nodeH / 2} L${b.x},${b.y - nodeH / 2}`);
      svg.appendChild(path);
    }

    const svgNS = "http://www.w3.org/2000/svg";
    for (const node of view.nodes) {
      const p = positions.get(node.id);
      if (!p) continue;
      const g = document.createElementNS(svgNS, "g");

      const rx = node.kind === "start" || node.kind === "end" ? nodeH / 2 : 8;
      const cls = ["gnode", node.kind, node.id === activeNode ? "active" : ""].join(" ").trim();
      const rect = document.createElementNS(svgNS, "rect");
      rect.setAttribute("class", cls);
      rect.setAttribute("x", p.x - nodeW / 2);
      rect.setAttribute("y", p.y - nodeH / 2);
      rect.setAttribute("width", nodeW);
      rect.setAttribute("height", nodeH);
      rect.setAttribute("rx", rx);

      const text = document.createElementNS(svgNS, "text");
      text.setAttribute("class", "glabel");
      text.setAttribute("x", p.x);
      text.setAttribute("y", p.y + 4);
      text.textContent = node.id + (node.kind === "interrupt" ? " ⏸" : "");

      g.appendChild(rect);
      g.appendChild(text);
      svg.appendChild(g);
    }
  }

  // ---- run detail: graph, checkpoints, cost, approvals -----------------

  async function loadGraph(sessionID) {
    try {
      const data = await fetchJSON(`/api/dashboard/graph?session_id=${encodeURIComponent(sessionID)}`);
      renderGraph(data.view, null);
    } catch (err) {
      els.graphSvg.innerHTML = "";
      const t = document.createElementNS("http://www.w3.org/2000/svg", "text");
      t.setAttribute("x", "10"); t.setAttribute("y", "20"); t.textContent = err.message;
      els.graphSvg.appendChild(t);
    }
  }

  async function loadCheckpoints(sessionID) {
    const data = await fetchJSON(`/api/dashboard/checkpoints?session_id=${encodeURIComponent(sessionID)}`);
    const cps = (data.checkpoints || []).slice().sort((a, b) => b.seq_num - a.seq_num);
    els.checkpointList.innerHTML = "";
    for (const cp of cps) {
      const li = document.createElement("li");
      const label = document.createElement("span");
      label.textContent = `seq ${cp.seq_num} → ${cp.node_id}`;
      const btn = document.createElement("button");
      btn.className = "secondary";
      btn.textContent = "time-travel";
      btn.onclick = (ev) => { ev.stopPropagation(); timeTravel(cp.id); };
      li.appendChild(label);
      li.appendChild(btn);
      els.checkpointList.appendChild(li);
    }
    if (cps.length) els.stateView.textContent = JSON.stringify(cps[0].state, null, 2);
  }

  async function loadCost(sessionID) {
    try {
      const report = await fetchJSON(`/api/dashboard/cost?session_id=${encodeURIComponent(sessionID)}`);
      els.costView.textContent =
        `${report.total_tokens || 0} tokens (${report.prompt_tokens || 0} prompt / ${report.completion_tokens || 0} completion) — ` +
        `$${(report.total_cost || 0).toFixed(4)} ${report.currency || ""}`;
    } catch (err) {
      els.costView.textContent = err.message;
    }
  }

  async function loadApprovals() {
    const data = await fetchJSON("/api/approval/pending");
    els.approvalList.innerHTML = "";
    for (const req of data.pending || []) {
      const li = document.createElement("li");
      const label = document.createElement("span");
      label.textContent = `${req.tool_name} (${req.id})`;
      li.appendChild(label);
      li.appendChild(document.createTextNode(" "));
      const approveBtn = document.createElement("button");
      approveBtn.textContent = "approve";
      approveBtn.onclick = () => respondApproval(req.id, true);
      const denyBtn = document.createElement("button");
      denyBtn.textContent = "deny";
      denyBtn.className = "deny";
      denyBtn.onclick = () => respondApproval(req.id, false);
      li.appendChild(approveBtn);
      li.appendChild(denyBtn);
      els.approvalList.appendChild(li);
    }
  }

  async function respondApproval(id, approved) {
    await fetchJSON("/api/approval/respond", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, approved }),
    });
    loadApprovals();
  }

  async function resumeSession() {
    if (!selected) return;
    await fetchJSON("/api/dashboard/resume", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id: selected }),
    });
    refreshRunDetail();
  }

  async function timeTravel(checkpointID) {
    await fetchJSON("/api/dashboard/timetravel", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ checkpoint_id: checkpointID }),
    });
    refreshRunDetail();
  }

  function refreshRunDetail() {
    if (!selected) return;
    loadSessions();
    loadGraph(selected);
    loadCheckpoints(selected);
    loadCost(selected);
    loadApprovals();
  }

  // ---- live stream (AG-UI) --------------------------------------------

  function logEvent(text) {
    const li = document.createElement("li");
    li.textContent = text;
    els.eventLog.prepend(li);
    while (els.eventLog.children.length > 200) els.eventLog.removeChild(els.eventLog.lastChild);
  }

  function connectStream(sessionID) {
    if (stream) stream.close();
    els.eventLog.innerHTML = "";
    stream = new EventSource(`/api/agui/stream?session=${encodeURIComponent(sessionID)}`);
    badge(els.connStatus, "connecting");
    stream.onopen = () => badge(els.connStatus, "live");
    stream.onerror = () => badge(els.connStatus, "disconnected");
    stream.onmessage = (msg) => {
      let evt;
      try { evt = JSON.parse(msg.data); } catch { return; }
      logEvent(`${evt.type} ${evt.stepName || evt.name || ""}`);
      switch (evt.type) {
        case "STEP_STARTED":
          fetchJSON(`/api/dashboard/graph?session_id=${encodeURIComponent(sessionID)}`)
            .then((data) => renderGraph(data.view, evt.stepName))
            .catch(() => {});
          break;
        case "RUN_FINISHED":
        case "RUN_ERROR":
          refreshRunDetail();
          break;
        case "CUSTOM":
          if (evt.name === "interrupt") refreshRunDetail();
          break;
      }
    };
  }

  // ---- selection --------------------------------------------------------

  function selectSession(id) {
    selected = id;
    els.emptyState.hidden = true;
    els.runView.hidden = false;
    els.runSessionId.textContent = id;
    els.resumeBtn.onclick = resumeSession;
    [...els.sessionList.children].forEach((li) => li.classList.toggle("selected", li.firstChild.textContent === id));
    refreshRunDetail();
    connectStream(id);
  }

  // ---- boot ---------------------------------------------------------------

  loadSessions().catch((err) => logEvent("error: " + err.message));
  setInterval(() => loadSessions().catch(() => {}), 5000);
})();
