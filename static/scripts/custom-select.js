// This component owns every custom selector in the application. It is intentionally
// independent from app.js so forms, reports, and feature pages all receive the same
// searchable dropdown behavior without adding more page-specific code to one large file.
(function registerCustomSelectComponent() {
  "use strict";

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
      panel.hidden = true;
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
      panel.hidden = false;
      selector.classList.add("is-open");
      trigger.setAttribute("aria-expanded", "true");
      const triggerBounds = trigger.getBoundingClientRect();
      const panelBounds = panel.getBoundingClientRect();
      const roomBelow = window.innerHeight - triggerBounds.bottom;
      selector.classList.toggle(
        "opens-upward",
        roomBelow < panelBounds.height + 16 && triggerBounds.top > roomBelow,
      );
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
