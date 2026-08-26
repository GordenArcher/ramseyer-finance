// This component owns every custom selector in the application. It is intentionally
// independent from app.js so forms, reports, and feature pages all receive the same
// searchable dropdown behavior without adding more page-specific code to one large file.
(function registerCustomSelectComponent() {
  "use strict";

  let selectorPanelSequence = 0;

  function queryAll(selector, root = document) {
    return Array.from(root.querySelectorAll(selector));
  }

  // bindCustomSelector activates selector markup that already exists in a template, such as
  // the backup library filters. Keyboard navigation, search, selection, and outside-click
  // dismissal all live here so handcrafted and generated selectors behave identically.
  window.bindCustomSelector = function bindCustomSelector(selector, onChange) {
    const trigger = selector.querySelector("[data-custom-selector-trigger]");
    const valueLabel = selector.querySelector("[data-custom-selector-value]");
    const panel = selector.querySelector("[data-custom-selector-panel]");
    const search = selector.querySelector("[data-custom-selector-search]");
    const emptyState = selector.querySelector("[data-custom-selector-empty]");
    if (!trigger || !valueLabel || !panel) {
      return;
    }
    const choices = queryAll("[data-value]", panel);
    if (choices.length === 0) {
      return;
    }
    const optionList = panel.querySelector(".custom-selector-options");
    let positionFrame = 0;
    if (!panel.id) {
      selectorPanelSequence += 1;
      panel.id = `custom-selector-panel-${selectorPanelSequence}`;
    }
    trigger.setAttribute("aria-controls", panel.id);

    // The panel is moved under document.body while open. A selector often lives inside a
    // modal, table card, or horizontally scrolling section whose overflow rules would clip
    // an ordinary absolutely positioned child. Portalling the panel to the window removes
    // that boundary while the stored element references keep selection state connected to
    // the original field.
    function positionPortalPanel() {
      if (panel.hidden || !selector.classList.contains("is-open")) {
        return;
      }

      const viewportMargin = 12;
      const panelGap = 8;
      const triggerBounds = trigger.getBoundingClientRect();
      if (
        triggerBounds.bottom < viewportMargin ||
        triggerBounds.top > window.innerHeight - viewportMargin
      ) {
        closeSelector();
        return;
      }

      const minimumWidth = selector.classList.contains("custom-form-selector")
        ? 280
        : 240;
      const panelWidth = Math.min(
        Math.max(triggerBounds.width, minimumWidth),
        window.innerWidth - viewportMargin * 2,
      );
      panel.style.width = `${panelWidth}px`;
      panel.style.maxHeight = "";
      if (optionList) {
        optionList.style.maxHeight = "";
      }

      const naturalPanelHeight = panel.getBoundingClientRect().height;
      const roomBelow =
        window.innerHeight - triggerBounds.bottom - panelGap - viewportMargin;
      const roomAbove = triggerBounds.top - panelGap - viewportMargin;
      const openAbove =
        roomBelow < naturalPanelHeight && roomAbove > roomBelow;
      const availableHeight = Math.max(96, openAbove ? roomAbove : roomBelow);

      // Only the option list should scroll. Constraining the entire panel would make the
      // search box disappear while browsing long lists, which is especially frustrating in
      // compact modals. I calculate the non-list chrome once, then give the remaining space
      // to the options with a small usable minimum for constrained windows.
      if (optionList && naturalPanelHeight > availableHeight) {
        const optionBounds = optionList.getBoundingClientRect();
        const fixedPanelHeight = naturalPanelHeight - optionBounds.height;
        optionList.style.maxHeight = `${Math.max(
          72,
          availableHeight - fixedPanelHeight,
        )}px`;
      }
      panel.style.maxHeight = `${availableHeight}px`;

      const positionedHeight = panel.getBoundingClientRect().height;
      const preferredLeft = selector.classList.contains(
        "custom-selector-align-end",
      )
        ? triggerBounds.right - panelWidth
        : triggerBounds.left;
      const left = Math.max(
        viewportMargin,
        Math.min(
          preferredLeft,
          window.innerWidth - panelWidth - viewportMargin,
        ),
      );
      const preferredTop = openAbove
        ? triggerBounds.top - positionedHeight - panelGap
        : triggerBounds.bottom + panelGap;
      const top = Math.max(
        viewportMargin,
        Math.min(
          preferredTop,
          window.innerHeight - positionedHeight - viewportMargin,
        ),
      );

      panel.style.left = `${left}px`;
      panel.style.top = `${top}px`;
      selector.classList.toggle("opens-upward", openAbove);
    }

    // Scroll events can originate from any nested card or modal and do not normally bubble.
    // A capture listener sees all of them, while requestAnimationFrame collapses a burst of
    // wheel events into one layout calculation so the floating panel follows its trigger
    // smoothly without doing expensive geometry work for every scroll event.
    function schedulePortalPosition() {
      if (positionFrame !== 0) {
        return;
      }
      positionFrame = window.requestAnimationFrame(() => {
        positionFrame = 0;
        positionPortalPanel();
      });
    }

    function stopPortalTracking() {
      window.removeEventListener("resize", schedulePortalPosition);
      window.removeEventListener("scroll", schedulePortalPosition, true);
      if (positionFrame !== 0) {
        window.cancelAnimationFrame(positionFrame);
        positionFrame = 0;
      }
    }

    function resetSearch() {
      if (search) {
        search.value = "";
      }
      choices.forEach((choice) => {
        choice.hidden = false;
      });
      if (emptyState) {
        emptyState.hidden = true;
      }
    }

    function closeSelector(returnFocus = false) {
      stopPortalTracking();
      panel.hidden = true;
      panel.classList.remove("is-portaled");
      panel.style.removeProperty("left");
      panel.style.removeProperty("top");
      panel.style.removeProperty("width");
      panel.style.removeProperty("max-height");
      optionList?.style.removeProperty("max-height");
      selector.classList.remove("is-open", "opens-upward");
      trigger.setAttribute("aria-expanded", "false");
      resetSearch();
      if (returnFocus) {
        trigger.focus();
      }
    }

    function openSelector() {
      queryAll("[data-custom-selector].is-open").forEach(
        (openSelectorElement) => {
          if (openSelectorElement !== selector) {
            openSelectorElement.dispatchEvent(
              new CustomEvent("custom-selector:close"),
            );
          }
        },
      );
      document.body.appendChild(panel);
      panel.classList.add("is-portaled");
      panel.hidden = false;
      selector.classList.add("is-open");
      trigger.setAttribute("aria-expanded", "true");
      positionPortalPanel();
      window.addEventListener("resize", schedulePortalPosition);
      window.addEventListener("scroll", schedulePortalPosition, true);
      window.requestAnimationFrame(() => search?.focus());
    }

    function chooseOption(choice) {
      selector.dataset.value = choice.dataset.value || "";
      valueLabel.textContent = choice.textContent.trim();
      choices.forEach((candidate) => {
        const selected = candidate === choice;
        candidate.classList.toggle("is-selected", selected);
        candidate.setAttribute("aria-selected", String(selected));
      });
      closeSelector(true);
      onChange?.(selector.dataset.value);
    }

    function focusAdjacentChoice(direction) {
      const visibleChoices = choices.filter(
        (choice) => !choice.hidden && !choice.disabled,
      );
      if (visibleChoices.length === 0) {
        return;
      }
      const currentIndex = visibleChoices.indexOf(document.activeElement);
      const nextIndex =
        currentIndex < 0
          ? 0
          : (currentIndex + direction + visibleChoices.length) %
            visibleChoices.length;
      visibleChoices[nextIndex].focus();
    }

    trigger.addEventListener("click", (event) => {
      event.stopPropagation();
      if (panel.hidden) {
        openSelector();
      } else {
        closeSelector();
      }
    });
    trigger.addEventListener("keydown", (event) => {
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
        return;
      }
      event.preventDefault();
      openSelector();
      window.requestAnimationFrame(() => {
        const selectedChoice = choices.find(
          (choice) =>
            choice.classList.contains("is-selected") && !choice.disabled,
        );
        (selectedChoice || choices.find((choice) => !choice.disabled))?.focus();
      });
    });

    search?.addEventListener("input", () => {
      const query = search.value.trim().toLowerCase();
      let visibleCount = 0;
      choices.forEach((choice) => {
        choice.hidden =
          query !== "" && !choice.textContent.toLowerCase().includes(query);
        if (!choice.hidden) {
          visibleCount += 1;
        }
      });
      if (emptyState) {
        emptyState.hidden = visibleCount !== 0;
      }
    });

    choices.forEach((choice) => {
      choice.addEventListener("click", () => chooseOption(choice));
    });
    panel.addEventListener("click", (event) => event.stopPropagation());
    panel.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        closeSelector(true);
      } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        if (document.activeElement !== search) {
          event.preventDefault();
          focusAdjacentChoice(event.key === "ArrowDown" ? 1 : -1);
        }
      } else if (
        event.key === "Enter" &&
        document.activeElement?.matches("[data-value]")
      ) {
        event.preventDefault();
        chooseOption(document.activeElement);
      }
    });
    selector.addEventListener("custom-selector:close", () => closeSelector());
    document.addEventListener("click", () => closeSelector());
  };

  // initCustomSelects progressively enhances every server-rendered select. The original
  // element remains a visually hidden form value, preserving validation and backend field
  // names while preventing a native dropdown from being displayed or opened.
  window.initCustomSelects = function initCustomSelects() {
    queryAll("select:not([data-custom-select-ready])").forEach(
      (select, index) => {
        if (select.options.length === 0) {
          return;
        }

        const selector = document.createElement("div");
        selector.className = "custom-selector custom-form-selector";
        selector.dataset.customSelector = "";
        selector.dataset.value = select.value;

        const trigger = document.createElement("button");
        trigger.type = "button";
        trigger.className = "custom-selector-trigger";
        trigger.dataset.customSelectorTrigger = "";
        trigger.setAttribute("aria-haspopup", "listbox");
        trigger.setAttribute("aria-expanded", "false");

        const valueLabel = document.createElement("span");
        valueLabel.dataset.customSelectorValue = "";
        const selectedOption = select.selectedOptions[0] || select.options[0];
        valueLabel.textContent =
          selectedOption?.textContent?.trim() || "Select an option";

        const chevron = document.createElement("span");
        chevron.className = "custom-selector-chevron";
        chevron.setAttribute("aria-hidden", "true");
        trigger.append(valueLabel, chevron);

        const panel = document.createElement("div");
        panel.className = "custom-selector-panel";
        panel.dataset.customSelectorPanel = "";
        panel.hidden = true;

        const fieldLabel = select.labels?.[0]?.textContent?.trim() || "options";
        const search = document.createElement("input");
        search.type = "search";
        search.placeholder = `Search ${fieldLabel.toLowerCase()}`;
        search.setAttribute("aria-label", `Search ${fieldLabel.toLowerCase()}`);
        search.autocomplete = "off";
        search.dataset.customSelectorSearch = "";
        trigger.setAttribute("aria-label", `Select ${fieldLabel.toLowerCase()}`);

        const optionList = document.createElement("div");
        optionList.className = "custom-selector-options";
        optionList.setAttribute("role", "listbox");
        optionList.setAttribute("aria-label", fieldLabel);

        Array.from(select.options).forEach((option) => {
          const choice = document.createElement("button");
          choice.type = "button";
          choice.className = "custom-selector-option";
          choice.dataset.value = option.value;
          choice.textContent = option.textContent.trim();
          choice.disabled = option.disabled;
          choice.setAttribute("role", "option");
          choice.setAttribute("aria-selected", String(option.selected));
          choice.classList.toggle("is-selected", option.selected);
          optionList.appendChild(choice);
        });

        const emptyState = document.createElement("small");
        emptyState.className = "custom-selector-empty";
        emptyState.dataset.customSelectorEmpty = "";
        emptyState.textContent = "No options match your search.";
        emptyState.hidden = true;
        panel.append(search, optionList, emptyState);
        selector.append(trigger, panel);

        select.dataset.customSelectReady = String(index + 1);
        select.classList.add("custom-select-source");
        select.insertAdjacentElement("afterend", selector);

        function syncFromNativeSelect() {
          const activeOption = select.selectedOptions[0] || select.options[0];
          selector.dataset.value = select.value;
          valueLabel.textContent =
            activeOption?.textContent?.trim() || "Select an option";
          queryAll("[data-value]", optionList).forEach((choice) => {
            const isSelected = choice.dataset.value === select.value;
            choice.classList.toggle("is-selected", isSelected);
            choice.setAttribute("aria-selected", String(isSelected));
          });
          selector.classList.remove("has-error");
        }

        window.bindCustomSelector(selector, (value) => {
          select.value = value;
          syncFromNativeSelect();
          select.dispatchEvent(new Event("input", { bubbles: true }));
          select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        select.addEventListener("change", syncFromNativeSelect);
        select.addEventListener("custom-select:sync", syncFromNativeSelect);
        select.addEventListener("invalid", (event) => {
          event.preventDefault();
          selector.classList.add("has-error");
          trigger.focus();
        });
        Array.from(select.labels || []).forEach((label) => {
          label.addEventListener("click", (event) => {
            event.preventDefault();
            trigger.focus();
          });
        });
      },
    );
  };
})();
