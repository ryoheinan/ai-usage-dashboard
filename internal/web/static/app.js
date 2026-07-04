const fmt = new Intl.NumberFormat();
const usd = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 4 });

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

function text(id, value) {
  document.getElementById(id).textContent = value;
}

function selectedFilters() {
  const rangeValue = document.getElementById("range").value;
  const source = document.getElementById("source").value;
  const params = new URLSearchParams();
  let label = `Last ${rangeValue} days`;
  if (rangeValue === "all") {
    params.set("range", "all");
    label = "All time";
  } else {
    params.set("days", rangeValue);
  }
  if (source !== "all") {
    params.set("source", source);
  }
  return { query: params.toString(), label };
}

function setRangeLabels(label) {
  for (const id of ["costRange", "tokensRange", "requestsRange", "failuresRange"]) {
    text(id, label);
  }
}

function renderChart(series) {
  const root = document.getElementById("chart");
  root.innerHTML = "";
  root.classList.toggle("chart-scroll", series.length > 14);
  const max = Math.max(1, ...series.map((p) => p.totalTokens));
  for (const point of series) {
    const item = document.createElement("div");
    item.className = "chart-item";

    const track = document.createElement("div");
    track.className = "bar-track";

    const bar = document.createElement("div");
    bar.className = point.totalTokens > 0 ? "bar" : "bar empty";
    bar.style.height = point.totalTokens > 0 ? `${Math.max(8, (point.totalTokens / max) * 100)}%` : "0";
    bar.title = `${point.bucket}: ${fmt.format(point.totalTokens)} tokens`;

    const label = document.createElement("span");
    label.className = "chart-label";
    label.textContent = point.bucket.slice(5).replace("-", "/");

    track.appendChild(bar);
    item.append(track, label);
    root.appendChild(item);
  }
}

function renderModels(rows) {
  const body = document.getElementById("models");
  body.innerHTML = "";
  for (const row of rows) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td></td><td></td><td>${fmt.format(row.totalTokens)}</td><td>${usd.format(row.estimatedCostUsd)}</td>`;
    tr.children[0].textContent = row.source;
    tr.children[1].textContent = row.model;
    body.appendChild(tr);
  }
  if (rows.length === 0) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td colspan="4">No telemetry yet</td>`;
    body.appendChild(tr);
  }
}

function renderSources(rows) {
  const body = document.getElementById("sources");
  body.innerHTML = "";
  for (const row of rows) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td></td><td>${fmt.format(row.requests)}</td><td>${fmt.format(row.totalTokens)}</td><td>${usd.format(row.estimatedCostUsd)}</td>`;
    tr.firstChild.textContent = row.source;
    body.appendChild(tr);
  }
  if (rows.length === 0) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td colspan="4">No telemetry yet</td>`;
    body.appendChild(tr);
  }
}

async function refresh() {
  const filters = selectedFilters();
  setRangeLabels(filters.label);
  const [summary, series, models, sources, health] = await Promise.all([
    getJSON(`/api/summary?${filters.query}`),
    getJSON(`/api/series?${filters.query}`),
    getJSON(`/api/breakdown/models?${filters.query}`),
    getJSON(`/api/breakdown/sources?${filters.query}`),
    getJSON("/api/health"),
  ]);

  text("cost", usd.format(summary.estimatedCostUsd));
  text("tokens", fmt.format(summary.totalTokens));
  text("requests", fmt.format(summary.requests));
  text("failures", fmt.format(summary.failures));
  text("input", fmt.format(summary.inputTokens));
  text("cached", fmt.format(summary.cachedInputTokens));
  text("cacheCreation", fmt.format(summary.cacheCreationTokens));
  text("output", fmt.format(summary.outputTokens));
  text("reasoning", fmt.format(summary.reasoningOutputTokens));
  text("lastEvent", health.lastEventAt ? new Date(health.lastEventAt).toLocaleString() : "Never");
  text("accepted", fmt.format(health.acceptedEvents));
  text("dropped", fmt.format(health.droppedContentFields));

  renderChart(series);
  renderModels(models);
  renderSources(sources);
}

function refreshDashboard() {
  refresh().catch(console.error);
}

function refreshWhenVisible() {
  if (document.visibilityState === "visible") {
    refreshDashboard();
  }
}

document.getElementById("refresh").addEventListener("click", refreshDashboard);
document.getElementById("range").addEventListener("change", refreshDashboard);
document.getElementById("source").addEventListener("change", refreshDashboard);
document.addEventListener("visibilitychange", refreshWhenVisible);
refreshDashboard();
