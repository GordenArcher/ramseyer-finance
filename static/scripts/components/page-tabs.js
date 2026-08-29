(function () {
  "use strict";

  const all = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  // activatePageTab switches long, independent report tables without moving the user down a
  // page. Page tabs have their own component because they live in the document flow, unlike
  // modal tabs, and must update both visibility and accessibility state together.
  function activatePageTab(button) {
    const container = button.closest("[data-page-tabs]");
    const targetId = button.dataset.pageTabTarget;
    if (!container || !targetId) return;

    all("[data-page-tab-target]", container).forEach((item) => {
      const active = item === button;
      item.classList.toggle("active", active);
      item.setAttribute("aria-selected", active ? "true" : "false");
      item.setAttribute("tabindex", active ? "0" : "-1");
    });
    all("[data-page-tab-panel]", container).forEach((panel) => {
      const active = panel.id === targetId;
      panel.classList.toggle("active", active);
      panel.hidden = !active;
    });
  }

  // Arrow keys make long Notes tab lists practical without requiring a mouse. Home and End
  // jump to the boundaries, matching the keyboard behaviour expected from an ARIA tab list.
  function initializePageTabs() {
    all("[data-page-tabs]").forEach((container) => {
      const buttons = all("[data-page-tab-target]", container);
      buttons.forEach((button, index) => {
        button.addEventListener("click", () => activatePageTab(button));
        button.addEventListener("keydown", (event) => {
          let nextIndex = index;
          if (event.key === "ArrowRight") nextIndex = (index + 1) % buttons.length;
          else if (event.key === "ArrowLeft") nextIndex = (index - 1 + buttons.length) % buttons.length;
          else if (event.key === "Home") nextIndex = 0;
          else if (event.key === "End") nextIndex = buttons.length - 1;
          else return;

          event.preventDefault();
          buttons[nextIndex].focus();
          activatePageTab(buttons[nextIndex]);
        });
      });
    });
  }

  document.addEventListener("DOMContentLoaded", initializePageTabs);
})();
