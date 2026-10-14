const svg = document.querySelector("#graph");
const detail = document.querySelector("#detail");
const stats = document.querySelector("#stats");
const W = 1200;
const H = 760;

const colors = {
  fact: "#4f7c2b",
  rule: "#3f7ea8",
  project: "#b0762a",
  preference: "#b23fae",
  concept: "#6d9a34",
  decision: "#356123",
  profile: "#8ab63f",
};

let vault = { nodes: [], edges: [] };
let topology = { nodes: [], edges: [] };
let positions = [];
let pins = new Map();
let localRoot = "";
let forces = { center: 0.1, repel: 1000, link: 1, distance: 250 };
let display = { node: 1, line: 1, fade: 0, arrows: false };
let layoutTimer = 0;
let hits = new Map();
let edgeHits = new Map();
let playhead = 0;
let maxDepth = 0;
let playing = null;
let selected = "";
let view = { x: 0, y: 0, k: 1 };

const hopColor = ["#70d7bd", "#ffc66d", "#ff8e6e", "#d38bff"];

document.querySelectorAll(".nav").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelectorAll(".nav").forEach((item) => item.classList.remove("active"));
    button.classList.add("active");
    const notes = button.dataset.view === "notes";
    document.querySelector("#graph-view").hidden = notes;
    document.querySelector("#notes-view").hidden = !notes;
    document.querySelector("h1").textContent = notes ? "笔记" : "知识图谱";
    if (notes) loadRecent();
  });
});

document.querySelector("#refresh").addEventListener("click", () => loadGraph());
document.querySelector("#fit").addEventListener("click", fit);
document.querySelector("#options").addEventListener("click", () => {
  document.querySelector("#options-panel").hidden = !document.querySelector("#options-panel").hidden;
});
document.querySelector("#all-notes").addEventListener("click", () => { localRoot = ""; applyView(); });
document.querySelector("#around").addEventListener("click", () => { if (selected) { localRoot = selected; applyView(); } });
["incoming", "outgoing", "unresolved", "isolated"].forEach((id) => {
  document.querySelector(`#${id}`).addEventListener("change", applyView);
});
document.querySelector("#filter").addEventListener("input", applyView);
["force-center", "force-repel", "force-link", "force-distance", "node-size", "line-size", "text-fade"].forEach((id) => {
  document.querySelector(`#${id}`).addEventListener("input", readForces);
});
document.querySelector("#arrows").addEventListener("change", readForces);
document.querySelector("#force-reset").addEventListener("click", () => {
  forces = { center: 0.1, repel: 1000, link: 1, distance: 250 };
  display = { node: 1, line: 1, fade: 0, arrows: false };
  document.querySelector("#force-center").value = "0.1";
  document.querySelector("#force-repel").value = "10";
  document.querySelector("#force-link").value = "1";
  document.querySelector("#force-distance").value = "250";
  document.querySelector("#node-size").value = "1";
  document.querySelector("#line-size").value = "1";
  document.querySelector("#text-fade").value = "0";
  document.querySelector("#arrows").checked = false;
  relax();
});

function readForces() {
  const repel = Number(document.querySelector("#force-repel").value);
  forces = {
    center: Number(document.querySelector("#force-center").value),
    repel: repel * repel * repel,
    link: Number(document.querySelector("#force-link").value),
    distance: Number(document.querySelector("#force-distance").value),
  };
  display = {
    node: Number(document.querySelector("#node-size").value),
    line: Number(document.querySelector("#line-size").value),
    fade: Number(document.querySelector("#text-fade").value),
    arrows: document.querySelector("#arrows").checked,
  };
  relax();
}
document.querySelector("#trace").addEventListener("click", trace);
document.querySelector("#query").addEventListener("keydown", (event) => {
  if (event.key === "Enter") trace();
});
document.querySelector("#clear").addEventListener("click", clearTrace);
document.querySelector("#play-reset").addEventListener("click", () => setPlayhead(0));
document.querySelector("#play-back").addEventListener("click", () => setPlayhead(playhead - 1));
document.querySelector("#play-step").addEventListener("click", () => setPlayhead(playhead + 1));
document.querySelector("#play-all").addEventListener("click", () => setPlayhead(maxDepth));
document.querySelector("#play").addEventListener("click", togglePlay);
document.querySelector("#remember").addEventListener("submit", remember);

svg.addEventListener("wheel", (event) => {
  event.preventDefault();
  view.k = Math.min(3.2, Math.max(0.45, view.k * (event.deltaY < 0 ? 1.08 : 0.92)));
  render();
}, { passive: false });

let drag = null;
svg.addEventListener("pointerdown", (event) => {
  const node = event.target.closest(".node");
  if (node) {
    const index = topology.nodes.findIndex((item) => item.id === node.dataset.id);
    drag = { kind: "node", id: node.dataset.id, index };
    return;
  }
  drag = { kind: "pan", x: event.clientX, y: event.clientY, ox: view.x, oy: view.y };
});
svg.addEventListener("pointermove", (event) => {
  if (!drag) return;
  if (drag.kind === "node") {
    positions[drag.index] = screenToGraph(event.clientX, event.clientY);
    return;
  }
  view.x = drag.ox + (event.clientX - drag.x);
  view.y = drag.oy + (event.clientY - drag.y);
  render();
});
svg.addEventListener("pointerup", () => { drag = null; });

function screenToGraph(clientX, clientY) {
  const rect = svg.getBoundingClientRect();
  const x = ((clientX - rect.left) / rect.width) * W;
  const y = ((clientY - rect.top) / rect.height) * H;
  return { x: (x - view.x) / view.k, y: (y - view.y) / view.k };
}

async function loadGraph() {
  const response = await fetch("/api/graph?limit=160");
  vault = await response.json();
  localRoot = "";
  pins = new Map();
  applyView();
}

function applyView() {
  const term = document.querySelector("#filter").value.trim().toLowerCase();
  const showUnresolved = document.querySelector("#unresolved").checked;
  const showIsolated = document.querySelector("#isolated").checked;
  const incoming = document.querySelector("#incoming").checked;
  const outgoing = document.querySelector("#outgoing").checked;
  const depth = Number(document.querySelector("#depth").value);
  let allowed = new Set(vault.nodes.filter((node) => {
    if (!showUnresolved && !node.resolved && !node.ambiguous) return false;
    if (!term) return true;
    return [node.title, node.category, node.path, ...(node.tags || [])].join(" ").toLowerCase().includes(term);
  }).map((node) => node.id));
  if (localRoot) {
    const around = neighborhood(localRoot, depth, incoming, outgoing);
    allowed = new Set([...around].filter((id) => allowed.has(id) || id === localRoot));
    allowed.add(localRoot);
  } else if (!showIsolated && !term) {
    const linked = new Set();
    for (const edge of vault.edges) {
      linked.add(edge.source);
      linked.add(edge.target);
    }
    allowed = new Set([...allowed].filter((id) => linked.has(id)));
  }
  topology = {
    ...vault,
    nodes: vault.nodes.filter((node) => allowed.has(node.id)),
    edges: vault.edges.filter((edge) => allowed.has(edge.source) && allowed.has(edge.target) && directionKept(edge, incoming, outgoing)),
  };
  document.querySelector("#all-notes").classList.toggle("active", !localRoot);
  document.querySelector("#around").classList.toggle("active", Boolean(localRoot));
  document.querySelector("#around").disabled = !selected;
  document.querySelector("#around").textContent = localRoot ? `${depth} 跳局部图` : "选中节点周围";
  positions = layout(topology.nodes, topology.edges);
  renderStats();
  render();
}

function neighborhood(root, depth, incoming, outgoing) {
  const seen = new Set([root]);
  let frontier = [root];
  for (let hop = 0; hop < depth; hop += 1) {
    const next = [];
    for (const id of frontier) {
      for (const edge of vault.edges) {
        let other = "";
        if (outgoing && edge.source === id) other = edge.target;
        if (incoming && edge.target === id) other = edge.source;
        if (!other || seen.has(other)) continue;
        seen.add(other);
        next.push(other);
      }
    }
    frontier = next;
  }
  return seen;
}

function directionKept(edge, incoming, outgoing) {
  if (incoming && outgoing) return true;
  if (!localRoot) return incoming || outgoing;
  if (outgoing && edge.source === localRoot) return true;
  if (incoming && edge.target === localRoot) return true;
  return false;
}

function fit() {
  view = { x: 0, y: 0, k: 1 };
  render();
}

function renderStats() {
  const cards = [
    ["笔记", topology.total_notes],
    ["链接", topology.total_edges],
    ["孤立", topology.isolated_count],
    ["未解析", topology.unresolved],
    ["图上", topology.nodes.length],
  ];
  stats.innerHTML = cards.map(([label, value]) => `<div><span>${label}</span><strong>${value ?? 0}</strong></div>`).join("");
}

function layout(nodes, edges) {
  const points = nodes.map((node, i) => {
    const angle = (i / Math.max(1, nodes.length)) * Math.PI * 2;
    const wobble = 1 + 0.28 * Math.sin(i * 2.399);
    const radius = Math.min(W, H) * 0.34;
    return { x: W / 2 + Math.cos(angle) * radius * wobble, y: H / 2 + Math.sin(angle) * radius * wobble };
  });
  const index = new Map(nodes.map((node, i) => [node.id, i]));
  const links = edges
    .map((edge) => [index.get(edge.source), index.get(edge.target)])
    .filter((pair) => pair[0] !== undefined && pair[1] !== undefined);
  for (let tick = 0; tick < 40; tick += 1) step(points, links, nodes);
  relax();
  return points;
}

function relax() {
  cancelAnimationFrame(layoutTimer);
  const run = () => {
    if (topology.nodes.length === 0) return;
    const index = new Map(topology.nodes.map((node, i) => [node.id, i]));
    const links = topology.edges
      .map((edge) => [index.get(edge.source), index.get(edge.target)])
      .filter((pair) => pair[0] !== undefined && pair[1] !== undefined);
    const dragIndex = drag && drag.kind === "node" ? drag.index : -1;
    step(positions, links, topology.nodes, dragIndex);
    render();
    layoutTimer = requestAnimationFrame(run);
  };
  layoutTimer = requestAnimationFrame(run);
}

function step(points, links, nodes, dragIndex) {
  for (let i = 0; i < points.length; i += 1) {
    for (let j = i + 1; j < points.length; j += 1) {
      let dx = points[i].x - points[j].x;
      let dy = points[i].y - points[j].y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const force = Math.min(8, forces.repel / (dist * dist));
      if (i !== dragIndex) {
        points[i].x += (dx / dist) * force;
        points[i].y += (dy / dist) * force;
      }
      if (j !== dragIndex) {
        points[j].x -= (dx / dist) * force;
        points[j].y -= (dy / dist) * force;
      }
    }
  }
  for (const [from, to] of links) {
    const dx = points[to].x - points[from].x;
    const dy = points[to].y - points[from].y;
    const dist = Math.sqrt(dx * dx + dy * dy) || 1;
    const force = (dist - forces.distance) * 0.02 * forces.link;
    if (from !== dragIndex) {
      points[from].x += (dx / dist) * force;
      points[from].y += (dy / dist) * force;
    }
    if (to !== dragIndex) {
      points[to].x -= (dx / dist) * force;
      points[to].y -= (dy / dist) * force;
    }
  }
  for (let i = 0; i < points.length; i += 1) {
    if (i === dragIndex) continue;
    points[i].x += (W / 2 - points[i].x) * forces.center * 0.08;
    points[i].y += (H / 2 - points[i].y) * forces.center * 0.08;
  }
}

function degreeColor(degree) {
  if (degree >= 12) return "#6e9eff";
  if (degree >= 6) return "#55b8a0";
  if (degree >= 3) return "#3f8a37";
  return "#74839b";
}

function revealed(id) {
  const depth = hits.get(id);
  return depth === undefined || depth > playhead ? undefined : depth;
}

function render() {
  const index = new Map(topology.nodes.map((node, i) => [node.id, i]));
  const tracing = hits.size > 0;
  const edges = topology.edges.map((edge) => {
    const a = positions[index.get(edge.source)];
    const b = positions[index.get(edge.target)];
    if (!a || !b) return "";
    const hop = edgeHits.get(`${edge.source}|${edge.target}`) ?? edgeHits.get(`${edge.target}|${edge.source}`);
    const shown = hop !== undefined && hop <= playhead;
    const dim = tracing && !shown;
    const color = shown ? hopColor[Math.min(hop, 3)] : "";
    const width = (0.8 + Math.min(edge.weight || 1, 4) * 0.35) * display.line;
    const marker = display.arrows ? ` marker-end="url(#arrow)"` : "";
    return `<line class="edge${dim ? " dim" : ""}" ${color ? `stroke="${color}"` : ""} stroke-width="${width.toFixed(2)}"${marker} x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}"></line>`;
  }).join("");
  const nodes = topology.nodes.map((node, i) => {
    const point = positions[i];
    const hop = revealed(node.id);
    const dim = tracing && hop === undefined ? " dim" : "";
    const selectedClass = node.id === selected ? " selected" : "";
    const unresolved = node.resolved ? "" : " unresolved";
    const radius = (4 + Math.min(14, Math.sqrt(node.degree || 0) * 2.4)) * display.node;
    const fill = hop === undefined ? degreeColor(node.degree || 0) : hopColor[Math.min(hop, 3)];
    const textAlpha = Math.min(1, Math.max(0, Math.log2(view.k) + 1 - display.fade));
    const showLabel = textAlpha > 0.08 && (node.id === selected || hop !== undefined || (node.degree || 0) >= 6 || view.k > 1.4);
    const label = showLabel
      ? `<text class="label" x="${point.x}" y="${point.y + radius + 14}" text-anchor="middle" opacity="${textAlpha.toFixed(2)}">${escapeHTML(shortTitle(node.title))}</text>`
      : "";
    return `<g class="node${dim}${selectedClass}${unresolved}" data-id="${escapeHTML(node.id)}">
      <circle cx="${point.x}" cy="${point.y}" r="${radius}" fill="${node.resolved ? fill : "none"}"></circle>${label}</g>`;
  }).join("");
  svg.innerHTML = `<defs><marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#8a907c"></path></marker></defs><g transform="translate(${view.x} ${view.y}) scale(${view.k})">${edges}${nodes}</g>`;
  svg.querySelectorAll(".node").forEach((node) => {
    node.addEventListener("click", () => select(node.dataset.id));
    node.addEventListener("pointerenter", () => hover(node.dataset.id));
    node.addEventListener("pointerleave", () => hover(""));
  });
}

function hover(id) {
  svg.querySelectorAll(".node, .edge").forEach((item) => item.classList.remove("near"));
  if (!id) return;
  const near = new Set([id]);
  for (const edge of topology.edges) {
    if (edge.source === id) near.add(edge.target);
    if (edge.target === id) near.add(edge.source);
  }
  svg.querySelectorAll(".node").forEach((node) => {
    if (near.has(node.dataset.id)) node.classList.add("near");
  });
}

function select(id) {
  selected = id;
  const node = topology.nodes.find((item) => item.id === id);
  if (!node) return;
  const neighbours = topology.edges
    .filter((edge) => edge.source === id || edge.target === id)
    .map((edge) => edge.source === id ? edge.target : edge.source);
  const linked = neighbours
    .map((nid) => topology.nodes.find((item) => item.id === nid))
    .filter(Boolean)
    .map((item) => `<span class="chip">${escapeHTML(item.title)}</span>`)
    .join("");
  document.querySelector("#around").disabled = false;
  detail.innerHTML = `
    <p class="eyebrow">${escapeHTML(node.category || "link")} · ${escapeHTML(node.tier || "")}</p>
    <h2>${escapeHTML(node.title)}</h2>
    <p class="muted">${node.resolved ? escapeHTML(node.path || "") : "这个链接还没有对应笔记。"}</p>
    <p>${(node.tags || []).map((tag) => `<span class="chip">#${escapeHTML(tag)}</span>`).join("")}</p>
    <p class="muted">相连</p>
    <p>${linked || '<span class="muted">没有链接</span>'}</p>`;
  render();
}

function shortTitle(title) {
  const text = String(title || "");
  return text.length > 18 ? `${text.slice(0, 17)}…` : text;
}

function setPlayhead(value) {
  playhead = Math.max(0, Math.min(maxDepth, value));
  document.querySelector("#playhead").textContent = playhead === 0 ? "种子" : `第 ${playhead} 跳 / ${maxDepth}`;
  render();
}

function togglePlay() {
  if (playing) {
    clearInterval(playing);
    playing = null;
    document.querySelector("#play").textContent = "播放";
    return;
  }
  if (playhead >= maxDepth) setPlayhead(0);
  document.querySelector("#play").textContent = "暂停";
  playing = setInterval(() => {
    if (playhead >= maxDepth) {
      togglePlay();
      return;
    }
    setPlayhead(playhead + 1);
  }, 700);
}

function clearTrace() {
  if (playing) togglePlay();
  hits = new Map();
  edgeHits = new Map();
  playhead = 0;
  maxDepth = 0;
  document.querySelector("#clear").hidden = true;
  document.querySelector("#playbar").hidden = true;
  document.querySelector("#elapsed").textContent = "";
  document.querySelector("#legend").innerHTML = `
    <span><i class="dot low"></i>1–2 条链接</span>
    <span><i class="dot mid"></i>3–5</span>
    <span><i class="dot high"></i>6–11</span>
    <span><i class="dot hub"></i>12+</span>
    <span><i class="ring"></i>未解析</span>`;
  render();
}

async function trace() {
  const query = document.querySelector("#query").value.trim();
  if (!query) return;
  const depth = Number(document.querySelector("#depth").value);
  const response = await fetch(`/api/recall?q=${encodeURIComponent(query)}&depth=${depth}`);
  const data = await response.json();
  hits = new Map();
  edgeHits = new Map();
  maxDepth = depth;
  for (const result of data.results || []) {
    if (!hits.has(result.entry.id)) hits.set(result.entry.id, 0);
    for (const hop of result.paths || []) {
      const hopDepth = Math.max(1, Number(hop.depth) || 1);
      const previous = hits.get(hop.to_id);
      if (previous === undefined || hopDepth < previous) hits.set(hop.to_id, hopDepth);
      if (!hits.has(hop.from_id)) hits.set(hop.from_id, Math.max(0, hopDepth - 1));
      edgeHits.set(`${hop.from_id}|${hop.to_id}`, hopDepth);
    }
  }
  document.querySelector("#clear").hidden = false;
  document.querySelector("#playbar").hidden = false;
  document.querySelector("#legend").innerHTML = `
    <span><i class="dot seed"></i>种子</span>
    <span><i class="dot hop1"></i>1 跳</span>
    <span><i class="dot hop2"></i>2 跳</span>
    <span><i class="dot hop3"></i>3 跳</span>`;
  document.querySelector("#elapsed").textContent = `${(data.results || []).length} 条 · ${data.elapsed}`;
  setPlayhead(0);
  detail.innerHTML = `
    <p class="eyebrow">召回 · ${escapeHTML(data.elapsed || "")}</p>
    <h2>${escapeHTML(query)}</h2>
    <ol class="hit-list">${(data.results || []).map((result) => `
      <li><strong>${escapeHTML(result.excerpt || result.entry.content)}</strong>
        <small>${escapeHTML(result.entry.category)} · ${escapeHTML(result.entry.tier)} · ${Number(result.score).toFixed(2)}${result.graph_score ? " · 图 " + Number(result.graph_score).toFixed(2) : ""}</small>
      </li>`).join("") || "<li>没有召回到笔记。</li>"}</ol>`;
  render();
}

async function remember(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const body = {
    content: form.content.value,
    category: form.category.value,
    tier: form.tier.value,
    links: form.links.value.split(/[,，]/).map((item) => item.trim()).filter(Boolean),
  };
  const response = await fetch("/api/remember", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    detail.textContent = await response.text();
    return;
  }
  form.reset();
  await loadGraph();
  await loadRecent();
}

async function loadRecent() {
  const response = await fetch("/api/recent");
  const notes = await response.json();
  document.querySelector("#recent").innerHTML = (notes || []).map((note) =>
    `<li>${escapeHTML(note.content)} <small>${escapeHTML(note.category)} · ${escapeHTML(note.tier)}</small></li>`
  ).join("");
}

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[char]));
}

loadGraph();
