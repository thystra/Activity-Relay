(() => {
  "use strict";

  const dashboard = document.querySelector("[data-relay-dashboard]");
  if (!dashboard) return;

  const byId = (id) => document.getElementById(id);
  const statusURL = dashboard.dataset.statusUrl || "/status.json";
  const health = byId("relay-health");
  const registration = byId("relay-registration");
  const count = byId("relay-instance-count");
  const receivingCount = byId("relay-receiving-count");
  const publisherCount = byId("relay-publisher-count");
  const version = byId("relay-version");
  const inbox = byId("relay-inbox-endpoint");
  const actor = byId("relay-actor-endpoint");
  const message = byId("relay-status-message");
  const list = byId("instance-list");
  const empty = byId("instance-empty");
  const search = byId("instance-search");
  const publisherList = byId("publisher-list");
  const publisherEmpty = byId("publisher-empty");
  const publisherSearch = byId("publisher-search");

  let domains = [];
  let publishers = [];

  function setText(element, value) {
    if (element) element.textContent = value;
  }

  function setStatusClass(element, className) {
    if (!element) return;
    element.classList.remove("status-good", "status-warning");
    element.classList.add(className);
  }

  function renderDomains(filter = "") {
    if (!list) return;
    const needle = filter.trim().toLowerCase();
    const visible = domains.filter((domain) => domain.includes(needle));
    list.replaceChildren();

    for (const domain of visible) {
      const item = document.createElement("li");
      item.textContent = domain;
      list.appendChild(item);
    }

    if (empty) empty.hidden = visible.length !== 0;
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
    if (!publisherList) return;
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
        ? (publisher.subscribed
          ? "Subscriber and publisher"
          : "Connected follower and publisher")
        : "Send-only publisher";
      heading.textContent = publisher.domain;
      meta.className = "publisher-meta";
      meta.textContent = `${role} · Last seen ${formatSeen(publisher.last_seen)} · ${publisher.activity_count ?? 0} accepted activities`;
      item.append(heading, meta);
      publisherList.appendChild(item);
    }

    if (publisherEmpty) publisherEmpty.hidden = visible.length !== 0;
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
      setText(health, data.status === "ok" ? "Online" : String(data.status ?? "Unknown"));
      setStatusClass(health, data.status === "ok" ? "status-good" : "status-warning");
      setText(registration, data.manual_approval ? "Approval required" : "Open");
      setText(count, String(data.connected_instances?.count ?? 0));
      setText(receivingCount, String(data.receiving_instances?.count ?? data.connected_instances?.count ?? 0));
      setText(publisherCount, String(data.publishers?.count ?? 0));
      setText(version, `${data.software?.name ?? "Activity-Relay"} ${data.software?.version ?? ""}`.trim());
      setText(inbox, data.endpoints?.inbox ?? "/inbox");
      setText(actor, data.endpoints?.actor ?? "/actor");

      domains = Array.isArray(data.connected_instances?.domains)
        ? data.connected_instances.domains.map((domain) => String(domain).toLowerCase())
        : [];
      publishers = Array.isArray(data.publishers?.entries)
        ? data.publishers.entries
        : [];

      renderDomains();
      renderPublishers();
      setText(message, `Status loaded from ${statusURL}.`);
    })
    .catch((error) => {
      setText(health, "Unavailable");
      setStatusClass(health, "status-warning");
      setText(registration, "Unknown");
      setText(count, "—");
      setText(receivingCount, "—");
      setText(publisherCount, "—");
      setText(version, "—");

      if (list) {
        list.innerHTML = '<li class="muted">The live participating-server list is temporarily unavailable.</li>';
      }
      if (publisherList) {
        publisherList.innerHTML = '<li class="muted">The live publisher list is temporarily unavailable.</li>';
      }
      setText(message, `Unable to load relay status: ${error.message}`);
    });
})();
