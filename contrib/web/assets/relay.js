(() => {
  "use strict";

  const dashboard = document.querySelector("[data-relay-dashboard]");
  if (!dashboard) return;

  const statusURL = dashboard.dataset.statusUrl || "/status.json";
  const health = document.getElementById("relay-health");
  const registration = document.getElementById("relay-registration");
  const count = document.getElementById("relay-instance-count");
  const version = document.getElementById("relay-version");
  const inbox = document.getElementById("relay-inbox-endpoint");
  const actor = document.getElementById("relay-actor-endpoint");
  const message = document.getElementById("relay-status-message");
  const list = document.getElementById("instance-list");
  const empty = document.getElementById("instance-empty");
  const search = document.getElementById("instance-search");

  let domains = [];

  function renderDomains(filter = "") {
    const needle = filter.trim().toLowerCase();
    const visible = domains.filter((domain) => domain.includes(needle));
    list.replaceChildren();

    for (const domain of visible) {
      const item = document.createElement("li");
      item.textContent = domain;
      list.appendChild(item);
    }

    empty.hidden = visible.length !== 0;
  }

  search?.addEventListener("input", () => renderDomains(search.value));

  fetch(statusURL, {
    headers: { Accept: "application/json" },
    credentials: "same-origin"
  })
    .then((response) => {
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return response.json();
    })
    .then((data) => {
      health.textContent = data.status === "ok" ? "Online" : data.status;
      health.classList.add(data.status === "ok" ? "status-good" : "status-warning");
      registration.textContent = data.manual_approval ? "Approval required" : "Open";
      count.textContent = String(data.connected_instances?.count ?? 0);
      version.textContent = `${data.software?.name ?? "Activity-Relay"} ${data.software?.version ?? ""}`.trim();
      inbox.textContent = data.endpoints?.inbox ?? "/inbox";
      actor.textContent = data.endpoints?.actor ?? "/actor";
      domains = Array.isArray(data.connected_instances?.domains)
        ? data.connected_instances.domains.map(String)
        : [];
      renderDomains();
      message.textContent = `Status loaded from ${statusURL}.`;
    })
    .catch((error) => {
      health.textContent = "Unavailable";
      health.classList.add("status-warning");
      registration.textContent = "Unknown";
      count.textContent = "—";
      version.textContent = "—";
      list.innerHTML = '<li class="muted">The live connected-server list is temporarily unavailable.</li>';
      message.textContent = `Unable to load relay status: ${error.message}`;
    });
})();
