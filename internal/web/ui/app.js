"use strict";

// Estado da aplicação.
const state = {
  cube: null,
  zones: { rows: [], cols: [], measures: [], filters: [] },
  samples: {}, // "dim|level" -> [membros de amostra]
  last: null,  // último resultado bruto
  view: "table", // table | bar | line
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
  const grid = $("#grid"), chart = $("#chart");
  if (state.view === "table") {
    chart.hidden = true; grid.hidden = false;
    if (state.zones.cols.length === 0) renderFlat(res);
    else renderPivot(res, state.zones.rows.length, state.zones.cols.length, state.zones.measures.length);
  } else {
    grid.hidden = true; chart.hidden = false;
    drawChart(state.view, buildChartData(res));
  }
}

// Tabela achatada (sem zona Colunas).
function renderFlat(res) {
  const cols = res.columns || [];
  const rows = res.rows || [];
  if (!cols.length) { $("#grid").innerHTML = '<div class="empty">sem dados</div>'; return; }
  state.gridCtx = { mode: "flat", rows, levels: state.zones.rows };
  const isMeasure = cols.map((c) => c.kind === "measure");
  let html = "<table><thead><tr>";
  cols.forEach((c, i) => (html += `<th class="${isMeasure[i] ? "measure" : ""}">${esc(c.name)}</th>`));
  html += "</tr></thead><tbody>";
  rows.forEach((row, ri) => {
    html += "<tr>";
    row.forEach((cell, i) => {
      const cls = isMeasure[i] ? "measure clickable" : "";
      const attr = isMeasure[i] ? ` data-row="${ri}"` : "";
      html += `<td class="${cls}"${attr}>${esc(cell.formatted)}</td>`;
    });
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

  state.gridCtx = { mode: "pivot", rowKeys, colKeys, rowLevels: state.zones.rows, colLevels: state.zones.cols };
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
  rowKeys.forEach((rk, rki) => {
    const rkS = rk.join(SEP);
    html += "<tr>";
    rk.forEach((v) => (html += `<td>${esc(v)}</td>`));
    colKeys.forEach((ck, cki) => {
      const ckS = ck.join(SEP);
      for (let m = 0; m < nMeas; m++) {
        const v = cell.get(rkS + SEP + ckS + SEP + m);
        html += `<td class="measure clickable" data-r="${rki}" data-c="${cki}">${esc(v == null ? "" : v)}</td>`;
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

// ---- gráficos (canvas nativo) ---------------------------------------------
const PALETTE = ["#5aa9ff", "#7ee0a0", "#c8a8ff", "#ffcf6b", "#ff8a8a", "#6be0d3", "#f7a072", "#a0c4ff"];

// Constrói categorias (eixo X = linhas) e séries (sem colunas: 1 por medida;
// com colunas: colKey × medida) a partir do resultado achatado.
function buildChartData(res) {
  const nRow = state.zones.rows.length, nCol = state.zones.cols.length, nMeas = state.zones.measures.length;
  const rows = res.rows || [];
  const measNames = state.zones.measures.map((m) => m.name);
  const SEP = String.fromCharCode(1);
  const cats = [], catIdx = new Map();
  const colKeys = [], colSeen = new Set();
  const vals = new Map();
  rows.forEach((r) => {
    const rk = []; for (let i = 0; i < nRow; i++) rk.push(r[i].formatted);
    const ck = []; for (let i = nRow; i < nRow + nCol; i++) ck.push(r[i].formatted);
    const rkS = rk.join(SEP) || "Total", ckS = ck.join(SEP);
    if (!catIdx.has(rkS)) { catIdx.set(rkS, cats.length); cats.push(rkS); }
    if (!colSeen.has(ckS)) { colSeen.add(ckS); colKeys.push(ck); }
    for (let m = 0; m < nMeas; m++) vals.set(catIdx.get(rkS) + SEP + ckS + SEP + m, num(r[nRow + nCol + m].value));
  });
  const series = [];
  const push = (name, ckS, m) => series.push({ name, values: cats.map((_, ci) => vals.get(ci + SEP + ckS + SEP + m) || 0) });
  if (nCol === 0) measNames.forEach((mn, m) => push(mn, "", m));
  else colKeys.forEach((ck) => measNames.forEach((mn, m) => push(ck.join(" / ") + (nMeas > 1 ? " · " + mn : ""), ck.join(SEP), m)));
  return { categories: cats.map((c) => c.split(SEP).join(" / ")), series };
}

function num(v) { const n = typeof v === "number" ? v : parseFloat(v); return isFinite(n) ? n : 0; }
function niceCeil(x) { const p = Math.pow(10, Math.floor(Math.log10(x))); const f = x / p; return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * p; }
function fmtNum(v) { if (v >= 1e6) return (v / 1e6).toFixed(1) + "M"; if (v >= 1e3) return (v / 1e3).toFixed(1) + "k"; return String(Math.round(v * 100) / 100); }

function drawChart(type, data) {
  const canvas = $("#chart");
  const dpr = window.devicePixelRatio || 1;
  const cssW = canvas.clientWidth || 820, cssH = canvas.clientHeight || 360;
  canvas.width = cssW * dpr; canvas.height = cssH * dpr;
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssW, cssH);
  ctx.font = "11px sans-serif";
  const { categories, series } = data;
  if (!categories.length || !series.length) { ctx.fillStyle = "#8b93a7"; ctx.fillText("sem dados", 20, 30); return; }

  const padL = 64, padR = 16, padT = 40, padB = 56;
  const x0 = padL, x1 = cssW - padR, y1 = cssH - padB, plotW = x1 - x0, plotH = y1 - padT;
  let max = 0; series.forEach((s) => s.values.forEach((v) => { if (v > max) max = v; }));
  const niceMax = niceCeil(max || 1);

  // gridlines + eixo Y
  ctx.strokeStyle = "#2a3040"; ctx.fillStyle = "#8b93a7"; ctx.textAlign = "right"; ctx.textBaseline = "middle";
  for (let g = 0; g <= 5; g++) {
    const y = y1 - plotH * g / 5;
    ctx.beginPath(); ctx.moveTo(x0, y); ctx.lineTo(x1, y); ctx.stroke();
    ctx.fillText(fmtNum(niceMax * g / 5), x0 - 6, y);
  }
  // legenda
  ctx.textAlign = "left"; ctx.textBaseline = "alphabetic"; let lx = padL, ly = 18;
  series.forEach((s, i) => {
    ctx.fillStyle = PALETTE[i % PALETTE.length]; ctx.fillRect(lx, ly - 8, 12, 12);
    ctx.fillStyle = "#dce1ea"; ctx.fillText(s.name, lx + 16, ly);
    lx += 30 + ctx.measureText(s.name).width; if (lx > x1 - 80) { lx = padL; ly += 16; }
  });
  // rótulos X
  const n = categories.length, slot = plotW / n;
  ctx.textAlign = "center"; ctx.textBaseline = "top"; ctx.fillStyle = "#8b93a7";
  categories.forEach((cat, ci) => {
    let label = cat.length > 14 ? cat.slice(0, 13) + "…" : cat;
    ctx.fillText(label, x0 + slot * (ci + 0.5), y1 + 6);
  });

  if (type === "bar") {
    const groupW = slot * 0.72, bw = groupW / series.length;
    categories.forEach((_, ci) => {
      const gx = x0 + slot * ci + (slot - groupW) / 2;
      series.forEach((s, si) => {
        const h = (s.values[ci] || 0) / niceMax * plotH;
        ctx.fillStyle = PALETTE[si % PALETTE.length];
        ctx.fillRect(gx + bw * si, y1 - h, Math.max(1, bw - 1), h);
      });
    });
  } else {
    series.forEach((s, si) => {
      const color = PALETTE[si % PALETTE.length];
      ctx.strokeStyle = color; ctx.lineWidth = 2; ctx.beginPath();
      s.values.forEach((v, ci) => {
        const cx = x0 + slot * (ci + 0.5), cy = y1 - (v / niceMax) * plotH;
        ci === 0 ? ctx.moveTo(cx, cy) : ctx.lineTo(cx, cy);
      });
      ctx.stroke();
      ctx.fillStyle = color;
      s.values.forEach((v, ci) => {
        const cx = x0 + slot * (ci + 0.5), cy = y1 - (v / niceMax) * plotH;
        ctx.beginPath(); ctx.arc(cx, cy, 3, 0, Math.PI * 2); ctx.fill();
      });
    });
  }
}

// ---- drill-through ---------------------------------------------------------
async function drillCell(contextFilters) {
  const filters = contextFilters.concat(
    state.zones.filters.map((f) => ({ dimension: f.dimension, level: f.level, members: f.members || [] }))
  );
  setStatus("drill-through…");
  const { ok, body } = await api("/saiku/api/query/drillthrough", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ cube: state.cube, filters, maxrows: 200 }),
  });
  if (!ok) { setStatus(body.error || "erro no drill-through", true); return; }
  setStatus(`${body.rows.length} linha(s) de fato`);
  openModal(body);
}

function gridCellFilters(td) {
  const ctx = state.gridCtx;
  if (!ctx) return null;
  const out = [];
  if (ctx.mode === "flat") {
    const row = ctx.rows[+td.dataset.row];
    ctx.levels.forEach((lv, k) => out.push({ dimension: lv.dimension, level: lv.level, members: [row[k].formatted] }));
  } else {
    const rk = ctx.rowKeys[+td.dataset.r], ck = ctx.colKeys[+td.dataset.c];
    ctx.rowLevels.forEach((lv, k) => out.push({ dimension: lv.dimension, level: lv.level, members: [rk[k]] }));
    ctx.colLevels.forEach((lv, k) => out.push({ dimension: lv.dimension, level: lv.level, members: [ck[k]] }));
  }
  return out;
}

function openModal(res) {
  const cols = res.columns || [], rows = res.rows || [];
  let html = "<table><thead><tr>" + cols.map((c) => `<th>${esc(c.name)}</th>`).join("") + "</tr></thead><tbody>";
  rows.forEach((r) => (html += "<tr>" + r.map((c) => `<td>${esc(c.formatted)}</td>`).join("") + "</tr>"));
  html += "</tbody></table>";
  if (!rows.length) html = '<div class="empty">nenhuma linha de fato neste contexto</div>';
  $("#modal-body").innerHTML = html;
  $("#modal").hidden = false;
}
function closeModal() { $("#modal").hidden = true; }

// ---- salvar / abrir consultas (localStorage) -------------------------------
function savedQueries() {
  try { return JSON.parse(localStorage.getItem("cubodw.queries") || "[]"); } catch { return []; }
}
function saveQuery() {
  if (!state.cube) return;
  const name = prompt("Nome da consulta:");
  if (!name) return;
  const list = savedQueries().filter((q) => q.name !== name);
  list.push({ name, cube: state.cube, zones: state.zones });
  localStorage.setItem("cubodw.queries", JSON.stringify(list));
  refreshOpenSelect();
  setStatus("consulta salva: " + name);
}
function refreshOpenSelect() {
  const sel = $("#open");
  sel.innerHTML = '<option value="">Abrir…</option>';
  savedQueries().forEach((q) => {
    const o = document.createElement("option");
    o.value = q.name; o.textContent = q.name;
    sel.appendChild(o);
  });
}
async function openQuery(name) {
  const q = savedQueries().find((x) => x.name === name);
  if (!q) return;
  $("#cube").value = q.cube;
  await loadSchema(q.cube);            // popula campos e zera as zonas
  state.zones = JSON.parse(JSON.stringify(q.zones));
  renderZones();
  setStatus("consulta aberta: " + name);
}

// ---- boot ------------------------------------------------------------------
setupZones();
$("#cube").addEventListener("change", (e) => loadSchema(e.target.value));
$("#run").addEventListener("click", run);
$("#toggle-sql").addEventListener("click", () => {
  const el = $("#sql"); el.hidden = !el.hidden;
});
document.querySelectorAll(".seg").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".seg").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    state.view = btn.dataset.view;
    if (state.last) renderResult(state.last);
  });
});
window.addEventListener("resize", () => {
  if (state.last && state.view !== "table") drawChart(state.view, buildChartData(state.last));
});
$("#grid").addEventListener("click", (e) => {
  const td = e.target.closest("td.clickable");
  if (!td) return;
  const filters = gridCellFilters(td);
  if (filters) drillCell(filters);
});
$("#save").addEventListener("click", saveQuery);
$("#open").addEventListener("change", (e) => { if (e.target.value) openQuery(e.target.value); });
$("#modal-close").addEventListener("click", closeModal);
$("#modal").addEventListener("click", (e) => { if (e.target.id === "modal") closeModal(); });
refreshOpenSelect();
loadCubes();
