(() => {
  const showMessage = (selector, text, isError) => {
    if (!selector) return;
    const node = document.querySelector(selector);
    if (!node) return;
    node.textContent = text || "";
    node.classList.toggle("is-error", Boolean(isError));
  };

  const refreshTarget = async (selector) => {
    const target = document.querySelector(selector);
    if (!target) return;
    const url = target.dataset.pollUrl;
    if (!url) return;
    const response = await fetch(url, {
      headers: {
        "X-FridgeFlow-Async": "1"
      },
      credentials: "same-origin"
    });
    if (!response.ok) return;
    target.innerHTML = await response.text();
  };

  const refreshTargets = async (selectors) => {
    const unique = Array.from(new Set((selectors || "").split(",").map((item) => item.trim()).filter(Boolean)));
    for (const selector of unique) {
      await refreshTarget(selector);
    }
  };

  document.addEventListener("submit", async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.matches("[data-async-form]")) {
      return;
    }

    event.preventDefault();
    showMessage(form.dataset.messageTarget, "", false);

    const response = await fetch(form.action, {
      method: (form.method || "post").toUpperCase(),
      body: new FormData(form),
      headers: {
        "X-FridgeFlow-Async": "1"
      },
      credentials: "same-origin"
    });

    if (!response.ok) {
      showMessage(form.dataset.messageTarget, await response.text(), true);
      return;
    }

    if (form.dataset.resetOnSuccess === "true") {
      form.reset();
    }

    await refreshTargets(form.dataset.refreshTargets);
  });

  document.querySelectorAll("[data-poll-url]").forEach((node) => {
    const seconds = Number(node.getAttribute("data-poll-interval") || "10");
    if (!Number.isFinite(seconds) || seconds <= 0) {
      return;
    }
    window.setInterval(() => {
      refreshTarget(`#${node.id}`);
    }, seconds * 1000);
  });

  if ("serviceWorker" in navigator) {
    window.addEventListener("load", () => {
      navigator.serviceWorker.register("/sw.js").catch(() => {});
    });
  }
})();
