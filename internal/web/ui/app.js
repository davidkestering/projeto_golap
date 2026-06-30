"use strict";

// Estado da aplicação.
const state = {
  cube: null,
  zones: { rows: [], measures: [], filters: [] },
  samples: {}, // "dim|level" -> [membros de amostra]
};

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
  state.zones = { rows: [], measures: [], filters: [] };
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
  return { "zone-rows": "rows", "zone-measures": "measures", "zone-filters": "filters" }[id];
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
  for (const [zoneId, key] of [["zone-rows", "rows"], ["zone-measures", "measures"], ["zone-filters", "filters"]]) {
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
  const payload = {
    cube: state.cube,
    measures: state.zones.measures.map((m) => m.name),
    rows: state.zones.rows.map((r) => ({ dimension: r.dimension, level: r.level })),
    filters: state.zones.filters.map((f) => ({ dimension: f.dimension, level: f.level, members: f.members || [] })),
  };
  setStatus("executando…");
  const { ok, body } = await api("/saiku/api/query", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!ok) { setStatus(body.error || "erro na consulta", true); return; }
  setStatus(`${body.rows.length} linha(s)`);
  $("#sql").textContent = body.sql || "";
  renderGrid(body);
}

function renderGrid(res) {
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
