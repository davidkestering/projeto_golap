"use strict";

// Estado da aplicação.
const state = {
  cube: null,
  zones: { rows: [], cols: [], measures: [], filters: [] },
  samples: {}, // "dim|level" -> [membros de amostra]
  last: null,  // último resultado bruto
};

// Zonas (id da div -> chave no estado).
const ZONES = [["zone-rows", "rows"], ["zone-cols", "cols"], ["zone-measures", "measures"], ["zone-filters", "filters"]];

const $ = (sel) => document.querySelector(sel);
const api = (path, opts) => fetch(path, opts).then((r) => r.json().then((j) => ({ ok: r.ok, body: j })));

function setStatus(msg, isErr) {
  const el = $("#status");
  el.textContent = msg || "";
  el.classList.toggle("err", !!isErr);
}

// ---- carga inicial: cubos --------------------------------------------------
async function loadCubes() {
  const { ok, body } = await api("/saiku/api/ai/cubes");
  if (!ok) return setStatus("falha ao listar cubos", true);
  const sel = $("#cube");
  sel.innerHTML = "";
  body.forEach((c) => {
    const o = document.createElement("option");
    o.value = c.cubeName;
    o.textContent = `${c.cubeName} (${c.measureCount} medidas)`;
    sel.appendChild(o);
  });
  if (body.length) loadSchema(body[0].cubeName);
}

// ---- schema do cubo: campos ------------------------------------------------
async function loadSchema(cube) {
  state.cube = cube;
  state.zones = { rows: [], cols: [], measures: [], filters: [] };
  state.samples = {};
  renderZones();
  setStatus("carregando schema…");
  const { ok, body } = await api("/saiku/api/ai/schema/" + encodeURIComponent(cube));
  if (!ok) return setStatus("falha ao carregar schema", true);

  // Medidas
  const mUl = $("#measures");
  mUl.innerHTML = "";
  (body.measures || []).forEach((m) => mUl.appendChild(fieldChip({ kind: "measure", name: m.name })));

  // Dimensões → níveis
  const dimWrap = $("#dimensions");
  dimWrap.innerHTML = "";
  (body.dimensions || []).forEach((d) => {
    const det = document.createElement("details");
    det.className = "dim";
    det.open = false;
    const sum = document.createElement("summary");
    sum.textContent = d.name;
    det.appendChild(sum);
    (d.hierarchies || []).forEach((h) =>
      (h.levels || []).forEach((l) => {
        const key = d.name + "|" + l.name;
        state.samples[key] = l.sampleMembers || [];
        det.appendChild(fieldChip({ kind: "level", dimension: d.name, level: l.name, name: l.name }));
      })
    );
    dimWrap.appendChild(det);
  });
  setStatus("");
}

// ---- chips arrastáveis (painel de campos) ----------------------------------
function fieldChip(field) {
  const li = document.createElement("li");
  li.className = "chip";
  li.dataset.kind = field.kind;
  li.textContent = field.name;
  li.draggable = true;
  li.addEventListener("dragstart", (e) => {
    e.dataTransfer.setData("application/json", JSON.stringify(field));
  });
  return li;
}

// ---- zonas de drop ---------------------------------------------------------
function setupZones() {
  document.querySelectorAll(".zone").forEach((zone) => {
    const accept = zone.dataset.accept;
    zone.addEventListener("dragover", (e) => {
      e.preventDefault();
      zone.classList.add("over");
    });
    zone.addEventListener("dragleave", () => zone.classList.remove("over"));
    zone.addEventListener("drop", (e) => {
      e.preventDefault();
      zone.classList.remove("over");
      let field;
      try { field = JSON.parse(e.dataTransfer.getData("application/json")); } catch { return; }
      if (field.kind !== accept) return setStatus(`esta zona aceita "${accept}"`, true);
      addToZone(zone.id, field);
    });
  });
}

function zoneKey(id) {
  return { "zone-rows": "rows", "zone-cols": "cols", "zone-measures": "measures", "zone-filters": "filters" }[id];
}

function addToZone(zoneId, field) {
  const key = zoneKey(zoneId);
  if (key === "filters") {
    const sk = field.dimension + "|" + field.level;
    const def = (state.samples[sk] || []).slice(0, 3).join(", ");
    const input = prompt(`Membros de ${field.dimension} · ${field.level} (separados por vírgula):`, def);
    if (input === null) return;
    field.members = input.split(",").map((s) => s.trim()).filter(Boolean);
  }
  // evita duplicados simples
  const exists = state.zones[key].some((f) =>
    key === "measures" ? f.name === field.name : f.dimension === field.dimension && f.level === field.level
  );
  if (!exists) state.zones[key].push(field);
  renderZones();
}

function renderZones() {
  for (const [zoneId, key] of ZONES) {
    const ul = $("#" + zoneId + " .dropchips");
    ul.innerHTML = "";
    state.zones[key].forEach((f, i) => {
      const li = document.createElement("li");
      li.className = "chip";
      li.dataset.kind = key === "measures" ? "measure" : "level";
      let label = key === "measures" ? f.name : `${f.dimension} · ${f.level}`;
      if (key === "filters" && f.members) label += " = " + f.members.join(", ");
      li.append(label);
      const x = document.createElement("span");
      x.className = "x"; x.textContent = "✕";
      x.onclick = () => { state.zones[key].splice(i, 1); renderZones(); };
      li.appendChild(x);
      ul.appendChild(li);
    });
  }
}

// ---- executar a consulta ---------------------------------------------------
async function run() {
  if (!state.cube) return;
  if (!state.zones.measures.length) return setStatus("arraste ao menos uma medida", true);
  const rowsLevels = state.zones.rows.map((r) => ({ dimension: r.dimension, level: r.level }));
  const colsLevels = state.zones.cols.map((c) => ({ dimension: c.dimension, level: c.level }));
  const payload = {
    cube: state.cube,
    measures: state.zones.measures.map((m) => m.name),
    rows: [...rowsLevels, ...colsLevels], // todos como group-by; o pivot é feito no cliente
    filters: state.zones.filters.map((f) => ({ dimension: f.dimension, level: f.level, members: f.members || [] })),
  };
  setStatus("executando…");
  const { ok, body } = await api("/saiku/api/query", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!ok) { setStatus(body.error || "erro na consulta", true); return; }
  state.last = body;
  setStatus(`${body.rows.length} linha(s)`);
  $("#sql").textContent = body.sql || "";
  renderResult(body);
}

function renderResult(res) {
  const nCol = state.zones.cols.length;
  if (nCol === 0) return renderFlat(res);
  renderPivot(res, state.zones.rows.length, nCol, state.zones.measures.length);
}

// Tabela achatada (sem zona Colunas).
function renderFlat(res) {
  const cols = res.columns || [];
  const rows = res.rows || [];
  if (!cols.length) { $("#grid").innerHTML = '<div class="empty">sem dados</div>'; return; }
  const isMeasure = cols.map((c) => c.kind === "measure");
  let html = "<table><thead><tr>";
  cols.forEach((c, i) => (html += `<th class="${isMeasure[i] ? "measure" : ""}">${esc(c.name)}</th>`));
  html += "</tr></thead><tbody>";
  rows.forEach((row) => {
    html += "<tr>";
    row.forEach((cell, i) => (html += `<td class="${isMeasure[i] ? "measure" : ""}">${esc(cell.formatted)}</td>`));
    html += "</tr>";
  });
  html += "</tbody></table>";
  $("#grid").innerHTML = html;
}

// Cross-tab: linhas (nRow níveis) × colunas (nCol níveis) × nMeas medidas.
// O resultado vem com as colunas na ordem: [níveis-linha…, níveis-coluna…, medidas…].
function renderPivot(res, nRow, nCol, nMeas) {
  const cols = res.columns || [];
  const rows = res.rows || [];
  const measNames = [];
  for (let i = nRow + nCol; i < cols.length; i++) measNames.push(cols[i].name);

  const SEP = "";
  const rowKeys = [], rowSeen = new Set();
  const colKeys = [], colSeen = new Set();
  const cell = new Map();
  rows.forEach((r) => {
    const rk = []; for (let i = 0; i < nRow; i++) rk.push(r[i].formatted);
    const ck = []; for (let i = nRow; i < nRow + nCol; i++) ck.push(r[i].formatted);
    const rkS = rk.join(SEP), ckS = ck.join(SEP);
    if (!rowSeen.has(rkS)) { rowSeen.add(rkS); rowKeys.push(rk); }
    if (!colSeen.has(ckS)) { colSeen.add(ckS); colKeys.push(ck); }
    for (let m = 0; m < nMeas; m++) cell.set(rkS + SEP + ckS + SEP + m, r[nRow + nCol + m].formatted);
  });

  const rowHead = state.zones.rows.map((r) => r.level);
  let html = "<table><thead><tr>";
  rowHead.forEach((n) => (html += `<th rowspan="${nMeas > 1 ? 2 : 1}">${esc(n)}</th>`));
  colKeys.forEach((ck) => {
    const label = esc(ck.join(" / "));
    html += nMeas > 1 ? `<th class="grp" colspan="${nMeas}">${label}</th>` : `<th class="measure">${label}</th>`;
  });
  html += "</tr>";
  if (nMeas > 1) {
    html += "<tr>";
    colKeys.forEach(() => measNames.forEach((mn) => (html += `<th class="measure">${esc(mn)}</th>`)));
    html += "</tr>";
  }
  html += "</thead><tbody>";
  rowKeys.forEach((rk) => {
    const rkS = rk.join(SEP);
    html += "<tr>";
    rk.forEach((v) => (html += `<td>${esc(v)}</td>`));
    colKeys.forEach((ck) => {
      const ckS = ck.join(SEP);
      for (let m = 0; m < nMeas; m++) {
        const v = cell.get(rkS + SEP + ckS + SEP + m);
        html += `<td class="measure">${esc(v == null ? "" : v)}</td>`;
      }
    });
    html += "</tr>";
  });
  html += "</tbody></table>";
  $("#grid").innerHTML = html;
}

function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}

// ---- boot ------------------------------------------------------------------
setupZones();
$("#cube").addEventListener("change", (e) => loadSchema(e.target.value));
$("#run").addEventListener("click", run);
$("#toggle-sql").addEventListener("click", () => {
  const el = $("#sql"); el.hidden = !el.hidden;
});
loadCubes();
