// Anvil UI. Plain JavaScript, no dependencies, so the page works under a strict
// same-origin Content-Security-Policy.
"use strict";

const form = document.getElementById("job-form");
const kindSelect = document.getElementById("kind");
const input = document.getElementById("input");
const formError = document.getElementById("form-error");
const tbody = document.getElementById("jobs");
const empty = document.getElementById("empty");

const labels = { wordcount: "Word count", uppercase: "Uppercase", reverse: "Reverse" };

async function api(path, options) {
  const res = await fetch(path, options);
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
  return body;
}

async function loadKinds() {
  const kinds = await api("/api/kinds");
  for (const kind of kinds) {
    const option = document.createElement("option");
    option.value = kind;
    option.textContent = labels[kind] || kind;
    kindSelect.append(option);
  }
}

// Builds rows with textContent only, so job input and results can never inject markup.
function render(jobs) {
  empty.hidden = jobs.length > 0;
  tbody.replaceChildren(
    ...jobs.map((job) => {
      const row = document.createElement("tr");
      const cells = [
        job.id,
        labels[job.kind] || job.kind,
        job.status,
        job.status === "failed" ? job.error : job.result || "…",
        new Date(job.createdAt).toLocaleTimeString(),
      ];
      for (const value of cells) {
        const cell = document.createElement("td");
        cell.textContent = value;
        row.append(cell);
      }
      row.dataset.status = job.status;
      return row;
    }),
  );
}

async function refresh() {
  try {
    render(await api("/api/jobs?limit=20"));
  } catch (err) {
    console.warn("refresh failed", err);
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  formError.hidden = true;
  try {
    await api("/api/jobs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ kind: kindSelect.value, input: input.value }),
    });
    input.value = "";
    await refresh();
  } catch (err) {
    formError.textContent = err.message;
    formError.hidden = false;
  }
});

loadKinds().catch((err) => {
  formError.textContent = `Could not reach the API: ${err.message}`;
  formError.hidden = false;
});
refresh();
setInterval(refresh, 2000);
