(() => {
  "use strict";

  const participating = document.getElementById("participating-count");
  const receiving = document.getElementById("receiving-count");
  const error = document.getElementById("status-error");

  fetch("/status.json", {
    headers: { Accept: "application/json" },
    credentials: "same-origin"
  })
    .then((response) => {
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return response.json();
    })
    .then((status) => {
      participating.textContent =
        String(status.connected_instances?.count ?? 0);
      receiving.textContent =
        String(
          status.receiving_instances?.count
          ?? status.connected_instances?.count
          ?? 0
        );
    })
    .catch((reason) => {
      participating.textContent = "Unavailable";
      receiving.textContent = "Unavailable";
      error.hidden = false;
      error.textContent =
        `Unable to load relay status: ${reason.message}`;
    });
})();
