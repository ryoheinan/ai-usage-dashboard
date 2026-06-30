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

function selectedRange() {
  const value = document.getElementById("range").value;
  if (value === "all") {
    return { query: "range=all", label: "All time" };
  }
  return { query: `days=${value}`, label: `Last ${value} days` };
}

function setRangeLabels(label) {
  for (const id of ["costRange", "tokensRange", "requestsRange", "failuresRange"]) {
    text(id, label);
  }
}

function renderChart(series) {
  const root = document.getElementById("chart");
  root.innerHTML = "";
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
    tr.innerHTML = `<td></td><td>${fmt.format(row.totalTokens)}</td><td>${usd.format(row.estimatedCostUsd)}</td>`;
    tr.firstChild.textContent = row.model;
    body.appendChild(tr);
  }
  if (rows.length === 0) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td colspan="3">No telemetry yet</td>`;
    body.appendChild(tr);
  }
}

async function refresh() {
  const range = selectedRange();
  setRangeLabels(range.label);
  const [summary, series, models, health] = await Promise.all([
    getJSON(`/api/summary?${range.query}`),
    getJSON(`/api/series?${range.query}`),
    getJSON(`/api/breakdown/models?${range.query}`),
    getJSON("/api/health"),
  ]);

  text("cost", usd.format(summary.estimatedCostUsd));
  text("tokens", fmt.format(summary.totalTokens));
  text("requests", fmt.format(summary.requests));
  text("failures", fmt.format(summary.failures));
  text("input", fmt.format(summary.inputTokens));
  text("cached", fmt.format(summary.cachedInputTokens));
  text("output", fmt.format(summary.outputTokens));
  text("reasoning", fmt.format(summary.reasoningOutputTokens));
  text("lastEvent", health.lastEventAt ? new Date(health.lastEventAt).toLocaleString() : "Never");
  text("accepted", fmt.format(health.acceptedEvents));
  text("dropped", fmt.format(health.droppedContentFields));

  renderChart(series);
  renderModels(models);
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
document.addEventListener("visibilitychange", refreshWhenVisible);
refreshDashboard();
