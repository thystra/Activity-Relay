(() => {
  "use strict";

  const dashboard = document.querySelector("[data-relay-dashboard]");
  if (!dashboard) return;
  const statusURL = dashboard.dataset.statusUrl || "/status.json";
  const health = document.getElementById("relay-health");
  const registration = document.getElementById("relay-registration");
  const count = document.getElementById("relay-instance-count");
  const publisherCount = document.getElementById("relay-publisher-count");
  const version = document.getElementById("relay-version");
  const inbox = document.getElementById("relay-inbox-endpoint");
  const actor = document.getElementById("relay-actor-endpoint");
  const message = document.getElementById("relay-status-message");
  const list = document.getElementById("instance-list");
  const empty = document.getElementById("instance-empty");
  const search = document.getElementById("instance-search");
  const publisherList = document.getElementById("publisher-list");
  const publisherEmpty = document.getElementById("publisher-empty");
  const publisherSearch = document.getElementById("publisher-search");
  let domains = [];
  let publishers = [];

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

  function formatSeen(value) {
    if (!value) return "Unknown";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short"
    }).format(date);
  }

  function renderPublishers(filter = "") {
    const needle = filter.trim().toLowerCase();
    const visible = publishers.filter((publisher) =>
      String(publisher.domain ?? "").toLowerCase().includes(needle)
    );
    publisherList.replaceChildren();

    for (const publisher of visible) {
      const item = document.createElement("li");
      const heading = document.createElement("strong");
      const meta = document.createElement("span");
      const role = publisher.receives_relay
        ? (publisher.subscribed ? "Subscriber and publisher" : "Connected follower and publisher")
        : "Send-only publisher";
      heading.textContent = publisher.domain;
      meta.className = "publisher-meta";
      meta.textContent = `${role} · Last seen ${formatSeen(publisher.last_seen)} · ${publisher.activity_count ?? 0} accepted activities`;
      item.append(heading, meta);
      publisherList.appendChild(item);
    }

    publisherEmpty.hidden = visible.length !== 0;
  }

  search?.addEventListener("input", () => renderDomains(search.value));
  publisherSearch?.addEventListener("input", () => renderPublishers(publisherSearch.value));
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
      publisherCount.textContent = String(data.publishers?.count ?? 0);
      version.textContent = `${data.software?.name ?? "Activity-Relay"} ${data.software?.version ?? ""}`.trim();
      inbox.textContent = data.endpoints?.inbox ?? "/inbox";
      actor.textContent = data.endpoints?.actor ?? "/actor";
      domains = Array.isArray(data.connected_instances?.domains)
        ? data.connected_instances.domains.map(String)
        : [];
      publishers = Array.isArray(data.publishers?.entries)
        ? data.publishers.entries
        : [];
      renderDomains();
      renderPublishers();
      message.textContent = `Status loaded from ${statusURL}.`;
    })
    .catch((error) => {
      health.textContent = "Unavailable";
      health.classList.add("status-warning");
      registration.textContent = "Unknown";
      count.textContent = "—";
      publisherCount.textContent = "—";
      version.textContent = "—";
      list.innerHTML = '<li class="muted">The live connected-server list is temporarily unavailable.</li>';
      publisherList.innerHTML = '<li class="muted">The live publisher list is temporarily unavailable.</li>';
      message.textContent = `Unable to load relay status: ${error.message}`;
    });
})();
