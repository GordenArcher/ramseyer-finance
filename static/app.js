// modalCloseTimers tracks the setTimeout handles for modal close animations so that
// rapid open/close cycles on the same modal don't stack conflicting timers. Each modal
// element maps to its close timeout ID. When a modal is opened while a close timer is
// still pending, the timer is cancelled to prevent it from closing the newly-opened modal.
const modalCloseTimers = new Map();

// pendingConfirmForm holds a reference to the form that triggered the confirmation modal,
// so the confirm button handler knows which form to re-submit after the user accepts.
// It is set when a confirm-gated form is submitted, and cleared when the modal closes
// or the confirmation is acted upon. Keeping it as a module-level variable allows the
// confirm modal to be reused by every destructive-action form on the page.
let pendingConfirmForm = null;

// qsa is a shorthand for querySelectorAll that always returns a real Array instead of a
// NodeList. This makes it ergonomic to use forEach, map, and filter on the results
// without needing to call Array.from at every call site. The optional root parameter
// allows scoping queries to a specific container element (e.g., a modal or form).
function qsa(selector, root = document) {
  return Array.from(root.querySelectorAll(selector));
}

// setPageReady adds a CSS class to the body that triggers the initial page-reveal
// animation. It uses requestAnimationFrame to ensure the class is added after the
// browser has had a chance to paint the initial (hidden) state, which makes CSS
// transitions from the hidden state to the visible state animate smoothly. Without the
// rAF delay, the browser might batch both states into a single paint and skip the
// transition entirely.
function setPageReady() {
  window.requestAnimationFrame(() => {
    document.body.classList.add("is-page-ready");
  });
}

// dismissAlert centralises alert removal so both the auto-dismiss timer and the manual close
// button use the same animated exit path. I guard against repeated calls because an alert can
// be auto-dismissing at the same time the user clicks the close button, and I do not want two
// overlapping timers racing to remove the same node.
function dismissAlert(alert) {
  if (!alert || alert.dataset.dismissing === "true") {
    return;
  }

  if (alert.dataset.dismissTimer) {
    window.clearTimeout(Number(alert.dataset.dismissTimer));
    delete alert.dataset.dismissTimer;
  }

  alert.dataset.dismissing = "true";
  alert.classList.add("is-dismissing");
  window.setTimeout(() => {
    alert.remove();
  }, 220);
}

// initAutoDismissAlerts finds all alert elements marked with the data-auto-dismiss-alert
// attribute, adds a visible close button, and schedules them to dismiss after a longer
// read-friendly delay. I keep this in shared JS so every server-rendered success/error
// banner behaves like the same toast component without repeating button markup across
// every template.
function initAutoDismissAlerts() {
  qsa("[data-auto-dismiss-alert]").forEach((alert) => {
    const existingText = alert.textContent.trim();
    alert.textContent = "";

    const body = document.createElement("span");
    body.className = "alert-body";
    body.textContent = existingText;

    const closeButton = document.createElement("button");
    closeButton.type = "button";
    closeButton.className = "alert-close";
    closeButton.setAttribute("aria-label", "Dismiss message");
    closeButton.innerHTML = "&times;";
    closeButton.addEventListener("click", () => dismissAlert(alert));

    alert.append(body, closeButton);

    const timer = window.setTimeout(() => {
      dismissAlert(alert);
    }, 5200);
    alert.dataset.dismissTimer = String(timer);
  });
}

// formatMoneyValue formats a numeric value using the browser's Intl.NumberFormat API
// with en-US locale settings, producing output like "1,234,567.89". Unlike the Go
// formatMoney function which always shows two decimal places, this JavaScript version
// uses minimumFractionDigits: 0 and maximumFractionDigits: 2, which means whole numbers
// display without decimals (e.g., "500") while fractional amounts show up to two
// decimal places. Non-finite values (NaN, Infinity) are returned as-is to avoid
// displaying "NaN" or "Infinity" in the UI.
function formatMoneyValue(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) {
    return String(value);
  }

  return new Intl.NumberFormat("en-US", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(numeric);
}

// initMoneyTooltips creates a single shared floating tooltip element that appears when
// the user hovers or focuses on a compact money value (elements with the .compact-money
// class). Instead of creating one tooltip node per value—which would be wasteful on
// pages with many compact amounts like the dashboard—a single reusable popover is
// positioned near the cursor or focused element. The tooltip displays the full,
// unformatted numeric value from the data-money-value attribute (or the element's
// original title attribute as a fallback) with a "GHC" currency prefix. On scroll, the
// tooltip is hidden to prevent it from drifting away from its target element.
function initMoneyTooltips() {
  const compactValues = qsa(
    ".compact-money[data-money-value], .compact-money[title]",
  );
  if (compactValues.length === 0) {
    return;
  }

  // I use one shared popover node for the whole page instead of one per amount because the
  // dashboard and reports can render many compact values. A single floating node is simpler,
  // lighter, and avoids multiple tooltip states fighting each other.
  const tooltip = document.createElement("div");
  tooltip.className = "money-popover";
  tooltip.setAttribute("aria-hidden", "true");
  document.body.appendChild(tooltip);

  function hideTooltip() {
    tooltip.classList.remove("is-visible");
    tooltip.setAttribute("aria-hidden", "true");
  }

  // positionTooltip places the tooltip near the mouse cursor or focus point, with
  // boundary detection that flips the tooltip to the left or above the cursor when
  // it would otherwise overflow the viewport. A 12px minimum margin from the viewport
  // edges prevents the tooltip from being clipped or touching the browser chrome.
  function positionTooltip(event) {
    const offsetX = 14;
    const offsetY = 18;
    const tooltipRect = tooltip.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    let left = event.clientX + offsetX;
    let top = event.clientY + offsetY;

    if (left + tooltipRect.width + 12 > viewportWidth) {
      left = event.clientX - tooltipRect.width - offsetX;
    }
    if (top + tooltipRect.height + 12 > viewportHeight) {
      top = event.clientY - tooltipRect.height - offsetY;
    }

    tooltip.style.left = `${Math.max(12, left)}px`;
    tooltip.style.top = `${Math.max(12, top)}px`;
  }

  function showTooltip(element, event) {
    const rawValue = element.dataset.moneyValue;
    if (!rawValue) {
      return;
    }

    tooltip.innerHTML = `
      <span class="money-popover-label">Actual Amount</span>
      <strong class="money-popover-value">GHC ${formatMoneyValue(rawValue)}</strong>
    `;
    tooltip.classList.add("is-visible");
    tooltip.setAttribute("aria-hidden", "false");
    positionTooltip(event);
  }

  // For each compact money element: copy the title attribute to a data attribute
  // (if not already set), remove the native title to prevent the browser's default
  // tooltip from appearing, make the element focusable via keyboard, and attach
  // mouse and focus event listeners for showing/hiding the custom tooltip.
  compactValues.forEach((element) => {
    if (!element.dataset.moneyValue) {
      element.dataset.moneyValue = element.getAttribute("title") || "";
    }
    element.removeAttribute("title");
    element.setAttribute("tabindex", "0");

    element.addEventListener("mouseenter", (event) => {
      showTooltip(element, event);
    });
    element.addEventListener("mousemove", (event) => {
      if (tooltip.classList.contains("is-visible")) {
        positionTooltip(event);
      }
    });
    element.addEventListener("mouseleave", hideTooltip);
    // Keyboard focus shows the tooltip centred below the element, providing an
    // accessible equivalent to the mouse hover behaviour for screen reader and
    // keyboard-only users.
    element.addEventListener("focus", () => {
      const rect = element.getBoundingClientRect();
      showTooltip(element, {
        clientX: rect.left + rect.width / 2,
        clientY: rect.bottom,
      });
    });
    element.addEventListener("blur", hideTooltip);
  });

  // Hide the tooltip on scroll to prevent it from appearing detached from its
  // target when the user scrolls while a tooltip is visible. The passive: true
  // option improves scroll performance by telling the browser the listener won't
  // call preventDefault().
  window.addEventListener("scroll", hideTooltip, { passive: true });
}

// startPageLoading adds a global loading indicator class to the body element. This
// typically triggers a CSS animation like a top-bar progress indicator or a subtle
// opacity change on the page content. It is called when the user navigates between
// pages via data-nav-loading links or when a form submit triggers a page-level action.
function startPageLoading() {
  document.body.classList.add("is-page-loading");
}

// stopPageLoading removes the global loading indicator class, restoring the page to
// its normal appearance. It is called after an export completes, a form submission
// finishes, or when an error occurs that should dismiss the loading state.
function stopPageLoading() {
  document.body.classList.remove("is-page-loading");
}

// applyLoadingFormState disables a form's submit button(s) and replaces their label
// with a loading indicator text (e.g., "Working..."). The original label is saved in
// a data attribute so it can be restored later. This prevents double-submission and
// provides visual feedback that the form is being processed. Both <button> and
// <input type="submit"> elements are handled, with different label-setting logic for
// each (textContent for buttons, value for inputs).
function applyLoadingFormState(form) {
  form.dataset.submitting = "true";
  form.classList.add("is-submitting");

  qsa('button[type="submit"], input[type="submit"]', form).forEach(
    (control) => {
      if (!control.dataset.originalLabel) {
        control.dataset.originalLabel =
          control.value || control.textContent.trim();
      }

      control.disabled = true;
      control.classList.add("is-loading");

      const loadingLabel = control.dataset.loadingLabel || "Working...";
      if (control.tagName === "INPUT") {
        control.value = loadingLabel;
      } else {
        control.textContent = loadingLabel;
      }
    },
  );
}

// restoreLoadingFormState reverses the effects of applyLoadingFormState: it re-enables
// the submit button(s), removes the loading CSS class, and restores the original label
// from the saved data attribute. It also clears any pending loading timer that was set
// to delay the page-level loading indicator. This function is called after a form
// submission completes (success or error) to return the form to an interactive state.
function restoreLoadingFormState(form) {
  if (form.dataset.loadingTimer) {
    window.clearTimeout(Number(form.dataset.loadingTimer));
    delete form.dataset.loadingTimer;
  }

  form.dataset.submitting = "";
  form.classList.remove("is-submitting");

  qsa('button[type="submit"], input[type="submit"]', form).forEach(
    (control) => {
      control.disabled = false;
      control.classList.remove("is-loading");

      if (control.tagName === "INPUT" && control.dataset.originalLabel) {
        control.value = control.dataset.originalLabel;
      }
      if (control.tagName === "BUTTON" && control.dataset.originalLabel) {
        control.textContent = control.dataset.originalLabel;
      }
    },
  );
}

// initNavigationLoading attaches click handlers to navigation links marked with
// data-nav-loading. When clicked with a normal left-click (not ctrl-click, middle-click,
// etc.), the page-level loading indicator is activated before the browser follows the
// link. This gives immediate visual feedback for page transitions, especially on slower
// connections or when the server takes time to render the next page.
function initNavigationLoading() {
  qsa("[data-nav-loading]").forEach((link) => {
    link.addEventListener("click", (event) => {
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      ) {
        return;
      }

      startPageLoading();
    });
  });
}

// initLoadingForms attaches submit handlers to forms marked with data-loading-form.
// When submitted, it applies the loading button state and starts a short timer (140ms)
// before showing the page-level loading indicator. The timer delay prevents a flash
// of the loading indicator on very fast submissions that complete almost instantly.
// Forms with data-confirm-submit are handled specially: the loading state is not
// applied until after the user confirms through the shared confirmation modal, so a
// cancelled confirmation leaves the button in its original, unaltered state.
function initLoadingForms() {
  qsa("form[data-loading-form]").forEach((form) => {
    form.addEventListener("submit", (event) => {
      if (form.dataset.submitting === "true") {
        event.preventDefault();
        return;
      }

      if (
        form.hasAttribute("data-confirm-submit") &&
        form.dataset.confirmedSubmit !== "true"
      ) {
        // I do not start the loading state yet for confirm-gated forms because a cancelled
        // delete or restore should leave the original button untouched.
        return;
      }

      if (event.defaultPrevented) {
        return;
      }

      applyLoadingFormState(form);
      const timer = window.setTimeout(startPageLoading, 140);
      form.dataset.loadingTimer = String(timer);
    });
  });
}

// ensureFormMessageNode finds or creates a .form-status paragraph element within a form,
// positioned just before the .form-actions container. This node is used by setFormMessage
// to display inline success/error feedback after export or restore operations without
// needing a page reload. If the node already exists, it is returned; otherwise it is
// created and inserted at the correct position in the form's DOM structure.
function ensureFormMessageNode(form) {
  let node = form.querySelector(".form-status");
  if (node) {
    return node;
  }

  node = document.createElement("p");
  node.className = "form-status";
  const actions = form.querySelector(".form-actions");
  form.insertBefore(node, actions);
  return node;
}

// setFormMessage updates the text content and tone (success/error) of a form's status
// message node. The tone is stored as a data attribute so CSS can apply appropriate
// colour coding (green for success, red for error). This provides inline feedback for
// async operations like exports without requiring a page redirect.
function setFormMessage(form, message, tone = "") {
  const node = ensureFormMessageNode(form);
  node.textContent = message;
  node.dataset.tone = tone;
}

// initExportForms intercepts the submit event on export forms (marked with
// data-export-form) and handles the export asynchronously via fetch instead of
// allowing a normal form submission. This is necessary because the desktop webview
// environment does not handle Content-Disposition attachment downloads reliably—
// it would replace the entire page with raw CSV/PDF content. By adding
// "delivery=native" to the request and processing the JSON response, the export is
// saved server-side and the UI shows the saved file path without navigating away.
function initExportForms() {
  qsa("form[data-export-form]").forEach((form) => {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      if (form.dataset.submitting === "true") {
        return;
      }

      setFormMessage(form, "");
      applyLoadingFormState(form);
      startPageLoading();

      try {
        // I intercept export submits because the desktop webview was replacing the whole screen
        // with raw CSV/PDF output. Saving server-side and returning the path is more reliable here.
        const formData = new FormData(form);
        const query = new URLSearchParams();
        for (const [key, value] of formData.entries()) {
          query.append(key, value);
        }
        query.set("delivery", "native");

        const response = await fetch(`${form.action}?${query.toString()}`, {
          credentials: "same-origin",
        });
        if (!response.ok) {
          const message = (await response.text()).trim() || "Export failed.";
          throw new Error(message);
        }

        const payload = await response.json();
        setFormMessage(
          form,
          payload.path ? `Export saved to ${payload.path}` : "Export saved.",
          "success",
        );
      } catch (error) {
        setFormMessage(
          form,
          error instanceof Error ? error.message : "Export failed.",
          "error",
        );
      } finally {
        restoreLoadingFormState(form);
        stopPageLoading();
      }
    });
  });
}

// initRestoreForms drives the in-app backup library. It filters and sorts the server-built
// candidate list without a round trip, keeps one selected restore source, and accepts an
// external file only through drag-and-drop. No click handler opens an operating-system or
// browser file dialog, which keeps the entire recovery workflow inside the app's own UI.
function initRestoreForms() {
  qsa("form[data-restore-form]").forEach((form) => {
    const library = form.querySelector("[data-backup-library]");
    const list = form.querySelector("[data-backup-list]");
    const options = qsa("[data-backup-option]", form);
    const searchInput = form.querySelector("[data-backup-search]");
    const kindFilter = form.querySelector("[data-backup-kind-filter]");
    const ageFilter = form.querySelector("[data-backup-age-filter]");
    const extensionFilter = form.querySelector(
      "[data-backup-extension-filter]",
    );
    const sortControl = form.querySelector("[data-backup-sort]");
    const resultCount = form.querySelector("[data-backup-result-count]");
    const selectedLabel = form.querySelector("[data-backup-selected-label]");
    const emptyState = form.querySelector("[data-backup-empty]");
    const dropZone = form.querySelector("[data-backup-drop-zone]");
    const droppedFileInput = form.querySelector("[data-restore-file-input]");
    const dropLabel = form.querySelector("[data-backup-drop-label]");

    function clearStatus() {
      const statusNode = form.querySelector(".form-status");
      if (statusNode) {
        statusNode.textContent = "";
        statusNode.dataset.tone = "";
      }
    }

    function clearLibrarySelection() {
      qsa("[data-backup-radio]", form).forEach((radio) => {
        radio.checked = false;
      });
      options.forEach((option) => option.classList.remove("is-selected"));
    }

    function clearDroppedFile() {
      if (droppedFileInput) {
        droppedFileInput.value = "";
      }
      if (dropLabel) {
        dropLabel.textContent = "No external file dropped.";
      }
      dropZone?.classList.remove("has-file");
    }

    function syncSelectedCandidate(radio) {
      clearStatus();
      options.forEach((option) => {
        option.classList.toggle(
          "is-selected",
          option.contains(radio) && radio.checked,
        );
      });
      clearDroppedFile();
      if (selectedLabel) {
        const item = radio.closest("[data-backup-option]");
        const name = item?.querySelector("strong")?.textContent?.trim();
        selectedLabel.textContent = name
          ? `Selected: ${name}`
          : "Backup selected";
      }
    }

    function selectedCandidateIsVisible() {
      const checked = form.querySelector("[data-backup-radio]:checked");
      if (!checked) {
        return true;
      }
      const item = checked.closest("[data-backup-option]");
      if (item && item.hidden) {
        clearLibrarySelection();
        if (selectedLabel) {
          selectedLabel.textContent = "No backup selected";
        }
        return false;
      }
      return true;
    }

    function applyLibraryFilters() {
      const query = searchInput?.value.trim().toLowerCase() || "";
      const selectedKind = kindFilter?.value || "all";
      const selectedAge = ageFilter?.value || "all";
      const selectedExtension = extensionFilter?.value || "all";
      const nowSeconds = Math.floor(Date.now() / 1000);
      let visibleCount = 0;

      options.forEach((option) => {
        const modified = Number(option.dataset.backupModified || 0);
        const ageDays = Math.max(0, (nowSeconds - modified) / 86400);
        const matchesQuery =
          !query || option.textContent.toLowerCase().includes(query);
        const matchesKind =
          selectedKind === "all" || option.dataset.backupKind === selectedKind;
        const matchesExtension =
          selectedExtension === "all" ||
          option.dataset.backupExtension === selectedExtension;
        const matchesAge =
          selectedAge === "all" || ageDays <= Number(selectedAge);
        option.hidden = !(
          matchesQuery &&
          matchesKind &&
          matchesExtension &&
          matchesAge
        );
        if (!option.hidden) {
          visibleCount += 1;
        }
      });

      if (resultCount) {
        resultCount.textContent = `${visibleCount} backup${visibleCount === 1 ? "" : "s"}`;
      }
      if (emptyState) {
        emptyState.hidden = visibleCount !== 0;
      }
      selectedCandidateIsVisible();
    }

    function sortLibrary() {
      if (!list) {
        return;
      }
      const mode = sortControl?.value || "newest";
      options
        .slice()
        .sort((left, right) => {
          if (mode === "name") {
            return left.textContent.trim().localeCompare(right.textContent.trim());
          }
          const leftTime = Number(left.dataset.backupModified || 0);
          const rightTime = Number(right.dataset.backupModified || 0);
          return mode === "oldest" ? leftTime - rightTime : rightTime - leftTime;
        })
        .forEach((option) => list.insertBefore(option, emptyState));
    }

    qsa("[data-backup-radio]", form).forEach((radio) => {
      radio.addEventListener("change", () => syncSelectedCandidate(radio));
    });
    [searchInput, kindFilter, ageFilter, extensionFilter].forEach((control) => {
      control?.addEventListener("input", applyLibraryFilters);
      control?.addEventListener("change", applyLibraryFilters);
    });
    sortControl?.addEventListener("change", () => {
      sortLibrary();
      applyLibraryFilters();
    });

    if (dropZone && droppedFileInput) {
      ["dragenter", "dragover"].forEach((eventName) => {
        dropZone.addEventListener(eventName, (event) => {
          event.preventDefault();
          dropZone.classList.add("is-dragging");
        });
      });
      ["dragleave", "drop"].forEach((eventName) => {
        dropZone.addEventListener(eventName, (event) => {
          event.preventDefault();
          dropZone.classList.remove("is-dragging");
        });
      });
      dropZone.addEventListener("drop", (event) => {
        clearStatus();
        const file = event.dataTransfer?.files?.[0];
        if (!file) {
          return;
        }
        if (!/\.(db|sqlite|sqlite3)$/i.test(file.name)) {
          setFormMessage(
            form,
            "Drop a .db, .sqlite, or .sqlite3 backup file.",
            "error",
          );
          return;
        }

        // DataTransfer gives the multipart form a real File object without calling
        // input.click(). That distinction is what makes this a custom drop workflow
        // rather than another hidden route back to the platform's native picker.
        try {
          const transfer = new DataTransfer();
          transfer.items.add(file);
          droppedFileInput.files = transfer.files;
        } catch {
          setFormMessage(
            form,
            "This app could not attach the dropped file. Move it into the Ramseyer Finance Backups folder, then refresh the library.",
            "error",
          );
          return;
        }
        clearLibrarySelection();
        dropZone.classList.add("has-file");
        if (dropLabel) {
          dropLabel.textContent = `External file: ${file.name}`;
        }
        if (selectedLabel) {
          selectedLabel.textContent = `Selected: ${file.name}`;
        }
      });
    }

    form.addEventListener("submit", (event) => {
      const hasLibrarySelection = Boolean(
        form.querySelector("[data-backup-radio]:checked"),
      );
      const hasDroppedFile = Boolean(
        droppedFileInput?.files && droppedFileInput.files.length > 0,
      );
      if (hasLibrarySelection || hasDroppedFile) {
        return;
      }

      event.preventDefault();
      setFormMessage(
        form,
        "Select a backup file before restoring data.",
        "error",
      );
      restoreLoadingFormState(form);
      stopPageLoading();
    });

    sortLibrary();
    applyLibraryFilters();
    library?.classList.add("is-ready");
  });
}

// initConfirmSubmits sets up the shared confirmation modal used by destructive actions
// (delete transaction, delete budget, restore backup). Forms marked with
// data-confirm-submit are intercepted on submit: instead of posting immediately, the
// confirmation modal is shown with a customisable title, message, and action label
// (read from data attributes on the form). When the user clicks the confirm button,
// the original form is re-submitted programmatically with a confirmedSubmit flag set,
// allowing the normal form pipeline (validation, loading states, server endpoint) to
// run as if the user had submitted directly. The modal's state is reset on close to
// prevent stale labels or disabled buttons from leaking between uses.
function initConfirmSubmits() {
  const confirmModal = document.getElementById("confirm-delete-modal");
  const confirmTitle = document.querySelector("[data-confirm-title]");
  const confirmMessage = document.querySelector("[data-confirm-message]");
  const confirmAccept = document.querySelector("[data-confirm-accept]");
  if (!confirmModal || !confirmTitle || !confirmMessage || !confirmAccept) {
    return;
  }

  function resetConfirmModalState() {
    // I hard-reset the shared confirm button every time because this modal is reused across
    // several destructive actions. Without this reset, a hidden or loading state can leak.
    pendingConfirmForm = null;
    confirmAccept.disabled = false;
    confirmAccept.classList.remove("is-loading");
    confirmTitle.textContent = "Confirm Action";
    confirmMessage.textContent = "This action cannot be undone.";
    confirmAccept.textContent = "Continue";
  }

  confirmModal.addEventListener("modal:close", resetConfirmModalState);

  qsa("form[data-confirm-submit]").forEach((form) => {
    form.addEventListener("submit", (event) => {
      if (form.dataset.confirmedSubmit === "true") {
        delete form.dataset.confirmedSubmit;
        return;
      }

      event.preventDefault();
      event.stopImmediatePropagation();
      pendingConfirmForm = form;
      resetConfirmModalState();
      pendingConfirmForm = form;
      confirmTitle.textContent = form.dataset.confirmTitle || "Confirm Action";
      confirmMessage.textContent =
        form.dataset.confirmMessage || "This action cannot be undone.";
      confirmAccept.textContent = form.dataset.confirmActionLabel || "Continue";
      openModal("confirm-delete-modal");
    });
  });

  confirmAccept.addEventListener("click", () => {
    if (!pendingConfirmForm) {
      closeModal(confirmModal);
      return;
    }

    // I re-submit the original form instead of doing the action directly from the modal so the
    // normal form pipeline still runs: validation, loading states, endpoints, and redirects.
    pendingConfirmForm.dataset.confirmedSubmit = "true";
    const form = pendingConfirmForm;
    closeModal(confirmModal);
    form.requestSubmit();
  });

  qsa("[data-close-modal]", confirmModal).forEach((button) => {
    button.addEventListener("click", resetConfirmModalState);
  });
}

// initBackupDownloads intercepts clicks on backup download links (marked with
// data-download-backup) and handles them asynchronously via fetch with
// "delivery=native", matching the pattern used by exports. This avoids the desktop
// webview replacing the page with raw binary content. On success or failure, a
// temporary status message is inserted next to the download link and auto-dismissed
// after 3 seconds with a fade-out animation.
function initBackupDownloads() {
  qsa("[data-download-backup]").forEach((link) => {
    link.addEventListener("click", async (event) => {
      event.preventDefault();
      if (link.dataset.submitting === "true") {
        return;
      }

      link.dataset.submitting = "true";
      const originalLabel = link.textContent.trim();
      link.dataset.originalLabel = originalLabel;
      link.classList.add("is-loading");
      link.textContent = "Preparing...";
      startPageLoading();

      try {
        // I keep backup download in JS for the same reason as exports: embedded webviews are
        // inconsistent about file downloads, but they are reliable at showing a saved-path result.
        const targetURL = new URL(link.href, window.location.origin);
        targetURL.searchParams.set("delivery", "native");
        const response = await fetch(targetURL.toString(), {
          credentials: "same-origin",
        });
        if (!response.ok) {
          const message =
            (await response.text()).trim() || "Backup download failed.";
          throw new Error(message);
        }

        const payload = await response.json();

        const messageNode = document.createElement("p");
        messageNode.className = "form-status";
        messageNode.dataset.tone = "success";
        messageNode.textContent = payload.path
          ? `Backup saved to ${payload.path}`
          : "Backup saved.";
        link.parentElement?.appendChild(messageNode);
        window.setTimeout(() => {
          messageNode.classList.add("is-dismissing");
          window.setTimeout(() => messageNode.remove(), 220);
        }, 3000);
      } catch (error) {
        const messageNode = document.createElement("p");
        messageNode.className = "form-status";
        messageNode.dataset.tone = "error";
        messageNode.textContent =
          error instanceof Error ? error.message : "Backup download failed.";
        link.parentElement?.appendChild(messageNode);
        window.setTimeout(() => {
          messageNode.classList.add("is-dismissing");
          window.setTimeout(() => messageNode.remove(), 220);
        }, 3000);
      } finally {
        link.dataset.submitting = "";
        link.classList.remove("is-loading");
        link.textContent = link.dataset.originalLabel || originalLabel;
        stopPageLoading();
      }
    });
  });
}

// initTableStages applies a staggered reveal animation to report tables marked with
// data-table-stage. Each table panel starts with an "is-table-loading" class that
// shows a skeleton or faded state, and after a staggered delay (220ms base + 90ms per
// panel index), it transitions to the "is-loaded" state. This creates a cascading
// reveal effect where tables appear to load in sequence, making the page feel more
// polished than if all tables appeared simultaneously.
function initTableStages() {
  qsa("[data-table-stage]").forEach((panel, index) => {
    // I add a short staged reveal even for local data so report tables feel intentionally loaded
    // instead of appearing abruptly all at once.
    panel.classList.add("is-table-loading");
    window.setTimeout(
      () => {
        panel.classList.add("is-loaded");
        panel.classList.remove("is-table-loading");
      },
      220 + index * 90,
    );
  });
}

// initUpdateCenter drives the in-app updater from the Setup page. The check button queries the
// backend for the latest GitHub Release, shows a status message when the current build is already
// current, and opens a modal with release notes when a newer version is available. Applying the
// update stages the Windows ZIP plus updater helper on the backend, then asks the native shell to
// terminate so the helper can replace the install directory and relaunch the app cleanly.
function initUpdateCenter() {
  const checkButton = document.querySelector("[data-check-updates]");
  const inlineStatus = document.querySelector("[data-update-status]");
  const updateModal = document.getElementById("update-modal");
  if (!checkButton || !inlineStatus || !updateModal) {
    return;
  }

  const titleNode = updateModal.querySelector("[data-update-modal-title]");
  const latestVersionNode = updateModal.querySelector(
    "[data-update-latest-version]",
  );
  const publishedAtNode = updateModal.querySelector("[data-update-published-at]");
  const notesNode = updateModal.querySelector("[data-update-notes]");
  const releaseLink = updateModal.querySelector("[data-update-release-link]");
  const applyButton = updateModal.querySelector("[data-apply-update]");
  const modalStatus = updateModal.querySelector("[data-update-modal-status]");
  const applyButtonDefaultLabel = applyButton
    ? applyButton.textContent.trim()
    : "Download and Apply Update";
  let canApplyUpdateInPlace = false;

  function setText(node, value) {
    if (node) {
      node.value = value;
    }
  }

  function setStatus(node, message, tone = "") {
    if (!node) {
      return;
    }
    node.textContent = message;
    node.dataset.tone = tone;
  }

  // resetUpdateModalState gives the update modal a clean baseline every time it is reopened. I do
  // this explicitly because the apply button can end a previous attempt in a disabled or loading
  // state, and I do not want that stale state to make the next release check look broken.
  function resetUpdateModalState() {
    setStatus(modalStatus, "");
    if (applyButton) {
      setButtonLoading(applyButton, false, "Preparing...");
      applyButton.disabled = false;
      applyButton.textContent = applyButtonDefaultLabel;
    }
    canApplyUpdateInPlace = false;
  }

  function setButtonLoading(button, loading, loadingLabel) {
    if (!button) {
      return;
    }
    if (!button.dataset.originalLabel) {
      button.dataset.originalLabel = button.textContent.trim();
    }

    button.disabled = loading;
    button.classList.toggle("is-loading", loading);
    button.textContent = loading
      ? loadingLabel
      : button.dataset.originalLabel || button.textContent;
  }

  async function fetchUpdateStatus() {
    const response = await fetch("/api/update/check", {
      credentials: "same-origin",
    });
    if (!response.ok) {
      const message = (await response.text()).trim() || "Update check failed.";
      throw new Error(message);
    }
    return response.json();
  }

  async function openReleasePage() {
    const releaseURL = releaseLink?.href || "";
    if (!releaseURL || releaseURL === "#") {
      throw new Error("No release page is available for this update yet.");
    }

    // I route external release links through the native shell first because embedded desktop
    // webviews are inconsistent about target=_blank. Falling back to window.open keeps browsers
    // working when the app is not running inside the packaged shell.
    if (typeof window.openExternalURL === "function") {
      await window.openExternalURL(releaseURL);
      return;
    }

    const popup = window.open(releaseURL, "_blank", "noopener,noreferrer");
    if (popup) {
      return;
    }

    window.location.href = releaseURL;
  }

  checkButton.addEventListener("click", async () => {
    setStatus(inlineStatus, "");
    setButtonLoading(checkButton, true, "Checking...");
    startPageLoading();

    try {
      const payload = await fetchUpdateStatus();
      if (!payload.update_available) {
        setStatus(inlineStatus, payload.message, "success");
        return;
      }

      titleNode.textContent = `Update ${payload.latest_version} available`;
      setText(latestVersionNode, payload.latest_version || "");
      setText(publishedAtNode, payload.published_at || "");
      setText(notesNode, payload.notes || "No release notes were published.");
      resetUpdateModalState();

      if (releaseLink) {
        releaseLink.href = payload.release_url || "#";
      }

      canApplyUpdateInPlace = Boolean(payload.can_apply);
      if (applyButton) {
        if (payload.can_apply) {
          applyButton.disabled = false;
          applyButton.textContent = applyButtonDefaultLabel;
        } else {
          applyButton.disabled = false;
          applyButton.textContent = "Open Windows Release";
        }
      }
      setStatus(
        modalStatus,
        payload.can_apply
          ? "The packaged Windows update can be downloaded and installed from here."
          : "This build can check releases, but in-app apply is only available inside the packaged Windows desktop app.",
        payload.can_apply ? "success" : "error",
      );
      openModal("update-modal");
    } catch (error) {
      setStatus(
        inlineStatus,
        error instanceof Error ? error.message : "Update check failed.",
        "error",
      );
    } finally {
      setButtonLoading(checkButton, false, "Checking...");
      stopPageLoading();
    }
  });

  if (!applyButton) {
    return;
  }

  if (releaseLink) {
    releaseLink.addEventListener("click", async (event) => {
      event.preventDefault();
      try {
        await openReleasePage();
      } catch (error) {
        setStatus(
          modalStatus,
          error instanceof Error ? error.message : "Could not open the release page.",
          "error",
        );
      }
    });
  }

  updateModal.addEventListener("modal:close", resetUpdateModalState);

  applyButton.addEventListener("click", async () => {
    if (!canApplyUpdateInPlace) {
      setStatus(
        modalStatus,
        "This environment can check for updates, but only the packaged Windows desktop app can apply them in place. Use the release page to download the new package.",
        "error",
      );
      try {
        await openReleasePage();
      } catch (error) {
        setStatus(
          modalStatus,
          error instanceof Error ? error.message : "Could not open the release page.",
          "error",
        );
      }
      return;
    }

    setStatus(modalStatus, "");
    setButtonLoading(applyButton, true, "Preparing...");
    startPageLoading();

    try {
      const response = await fetch("/api/update/apply", {
        method: "POST",
        credentials: "same-origin",
      });
      if (!response.ok) {
        const message = (await response.text()).trim() || "Update apply failed.";
        throw new Error(message);
      }

      const payload = await response.json();
      setStatus(modalStatus, payload.message || "Update prepared.", "success");

      if (payload.quit_required && typeof window.quitApp === "function") {
        // I leave a short readable pause here so the operator sees that the update was staged
        // successfully before the desktop shell closes and the updater takes over.
        window.setTimeout(() => {
          window.quitApp();
        }, 1100);
      }
    } catch (error) {
      setStatus(
        modalStatus,
        error instanceof Error ? error.message : "Update apply failed.",
        "error",
      );
    } finally {
      setButtonLoading(applyButton, false, "Preparing...");
      stopPageLoading();
    }
  });
}

// switchModal moves the user directly from one modal workflow into another without leaving
// both shells visible at the same time. I use it for the entry chooser so the dedicated
// data-entry route can ask "which console?" first, then hand off straight into the selected
// finance or asset modal with one smooth interaction.
function switchModal(trigger) {
  const targetId = trigger.dataset.switchModal;
  if (!targetId) {
    return;
  }

  const currentModal = trigger.closest(".modal-shell");
  if (currentModal) {
    closeModal(currentModal);
    window.setTimeout(() => openModal(targetId), 170);
    return;
  }

  openModal(targetId);
}

// initCategoryEditor keeps the shared category modal honest when it is reused for both
// "edit existing category" and "add new category". The server-rendered edit flow deliberately
// hydrates the modal with the selected row's values, but that means a plain client-side
// reopen would otherwise keep showing the last edited category. I reset the mutable fields
// explicitly on "Add Category" so create mode always starts from a blank chart-of-accounts
// form, and I also restore the base paginated URL so the edit query does not linger.
function initCategoryEditor() {
  const modal = document.getElementById("category-editor");
  if (!modal) {
    return;
  }

  const form = modal.querySelector("form[data-category-form]");
  const title = modal.querySelector("[data-category-modal-title]");
  const submitButton = modal.querySelector("[data-category-submit-label]");
  const returnTo = modal.dataset.modalReturnTo || "/categories?page=1";
  if (!form || !title || !submitButton) {
    return;
  }

  function resetCategoryFormForCreate() {
    // I reset field-by-field instead of calling form.reset() because reset would restore the
    // server-rendered edit values when the page was opened with ?edit=..., which is exactly
    // the stale-state bug this handler is meant to prevent.
    const idField = form.querySelector('input[name="id"]');
    const typeField = form.querySelector('select[name="type"]');
    const nameField = form.querySelector('input[name="name"]');
    const parentField = form.querySelector('select[name="parent_id"]');
    const noteRefField = form.querySelector('input[name="note_ref"]');
    const reportSectionField = form.querySelector('select[name="report_section"]');
    const returnField = form.querySelector('input[name="return_to"]');

    if (idField) {
      idField.value = "";
    }
    if (typeField) {
      typeField.value = "";
    }
    if (nameField) {
      nameField.value = "";
    }
    if (parentField) {
      parentField.value = "";
    }
    if (noteRefField) {
      noteRefField.value = "";
    }
    if (reportSectionField) {
      reportSectionField.value = "";
    }
    if (returnField) {
      returnField.value = returnTo;
    }

    title.textContent = "Add Category";
    submitButton.textContent = "Save Category";

    // I clear the stale edit query as soon as the user chooses "Add Category" so refreshes,
    // closes, and later modal opens all stay aligned with the create-mode intent.
    window.history.replaceState({}, "", returnTo);
  }

  qsa("[data-category-create-trigger]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      resetCategoryFormForCreate();
    });
  });
}

// openModal shows a modal dialog by its element ID. It cancels any pending close timer
// on the modal (in case it was in the process of closing), adds the visibility and
// open classes to trigger CSS transitions, sets aria-hidden to false for accessibility,
// adds a has-modal class to the body (which typically prevents background scrolling),
// and focuses the first form field inside the modal after a short delay. The focus
// delay (160ms) allows the open animation to settle first, preventing some webview
// shells from jump-scrolling before the modal transition completes.
function openModal(id) {
  const modal = document.getElementById(id);
  if (!modal) {
    return;
  }

  const timer = modalCloseTimers.get(modal);
  if (timer) {
    window.clearTimeout(timer);
    modalCloseTimers.delete(modal);
  }

  modal.classList.add("is-visible");
  modal.classList.remove("is-closing");
  modal.setAttribute("aria-hidden", "false");
  document.body.classList.add("has-modal");

  window.requestAnimationFrame(() => {
    modal.classList.add("is-open");
  });

  // I delay focus slightly so the open animation can settle first. Immediate focus was causing
  // some shells to jump-scroll before the modal finished transitioning.
  const firstField = modal.querySelector("input, select, textarea, button");
  if (firstField) {
    window.setTimeout(() => firstField.focus(), 160);
  }
}

// closeModal hides a modal dialog with a closing animation. It dispatches a custom
// "modal:close" event so that other code (like the confirm modal reset logic) can
// react to the modal closing. The modal transitions through an "is-closing" state
// for 260ms (matching the CSS transition duration), after which the visibility
// classes are fully removed. If no other visible modals remain, the has-modal class
// is removed from the body to restore normal page scrolling.
function closeModal(modal) {
  if (!modal || modal.getAttribute("aria-hidden") === "true") {
    return;
  }

  if (modal.dataset.clearEditQuery === "true" && modal.dataset.modalReturnTo) {
    // I replace the current URL on close for edit-backed modals so closing the popup actually
    // returns the page to its non-edit state instead of leaving a stale ?edit=... bookmark behind.
    window.history.replaceState({}, "", modal.dataset.modalReturnTo);
  }

  modal.dispatchEvent(new CustomEvent("modal:close"));
  modal.classList.remove("is-open");
  modal.classList.add("is-closing");
  modal.setAttribute("aria-hidden", "true");

  const timer = window.setTimeout(() => {
    modal.classList.remove("is-visible", "is-closing");
    modalCloseTimers.delete(modal);
    if (!document.querySelector(".modal-shell.is-visible")) {
      document.body.classList.remove("has-modal");
    }
  }, 260);

  modalCloseTimers.set(modal, timer);
}

// activateTab switches between tab panels within a modal or container. It finds the
// parent .modal-panel container, toggles the "active" class on the tab buttons and
// the corresponding panel identified by the button's data-tab-target attribute. This
// allows multi-tab forms (like the finance entry modal with income/expenditure/asset/
// liability tabs) to share a single container while showing only one panel at a time.
function activateTab(button) {
  const container = button.closest(".modal-panel");
  if (!container) {
    return;
  }

  const targetId = button.dataset.tabTarget;
  if (!targetId) {
    return;
  }

  qsa(".modal-tab", container).forEach((item) => {
    item.classList.toggle("active", item === button);
  });
  qsa(".modal-form-panel", container).forEach((panel) => {
    panel.classList.toggle("active", panel.id === targetId);
  });
}

// initModals sets up all modal-related behaviour: open triggers (buttons with
// data-open-modal that reference a modal ID), close buttons (elements with
// data-close-modal inside each modal), tab switching within modals, Escape key
// dismissal (closes the currently open modal), and auto-opening of a modal on page
// load (elements with data-auto-open-modal, used when redirecting with ?open=modal-id).
// Each modal initialises with aria-hidden="true" so screen readers treat it as hidden
// until explicitly opened.
function initModals() {
  qsa("[data-open-modal]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      openModal(trigger.dataset.openModal);
    });
  });

  qsa("[data-switch-modal]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      switchModal(trigger);
    });
  });

  qsa(".modal-shell").forEach((modal) => {
    modal.setAttribute("aria-hidden", "true");
    qsa("[data-close-modal]", modal).forEach((closer) => {
      closer.addEventListener("click", () => closeModal(modal));
    });
  });

  qsa(".modal-tab").forEach((button) => {
    button.addEventListener("click", () => activateTab(button));
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      const modal = document.querySelector(".modal-shell.is-open");
      if (modal) {
        closeModal(modal);
      }
    }
  });

  const autoOpen = document.querySelector("[data-auto-open-modal]");
  if (autoOpen) {
    window.setTimeout(() => openModal(autoOpen.dataset.autoOpenModal), 180);
  }
}

// ensureStatusNode finds or creates a .auth-status paragraph element within the PIN
// authentication form, positioned just before the .auth-actions container. This node
// displays feedback messages during PIN setup and unlock (e.g., "Confirm your new PIN"
// or "Incorrect PIN"). If the node already exists, it is returned; otherwise it is
// created and inserted.
function ensureStatusNode(form) {
  let node = form.querySelector(".auth-status");
  if (node) {
    return node;
  }

  node = document.createElement("p");
  node.className = "auth-status";
  const actions = form.querySelector(".auth-actions");
  form.insertBefore(node, actions);
  return node;
}

// setStatus updates the text content and tone of the PIN screen's status message.
// The tone is stored as a data attribute for CSS styling (e.g., "error" for red text,
// "info" for blue text during PIN confirmation).
function setStatus(form, message, tone = "") {
  const node = ensureStatusNode(form);
  node.textContent = message;
  node.dataset.tone = tone;
}

// bindPinScreen wires up the interactive PIN entry keypad for a single PIN form. It
// manages a local pinValue variable that tracks the entered digits, syncs the visible
// dot indicators (via the .is-filled class), handles digit button clicks, clear and
// backspace buttons, keyboard input (0-9, Backspace, Escape, Enter), and implements
// the two-pass PIN setup confirmation flow. During setup mode (data-auth-mode="setup"),
// the first submission captures the PIN locally, clears the display, and prompts the
// user to re-enter; the second submission compares against the first pass. If they
// match, the form submits normally; if not, the process resets. This keeps the setup
// flow entirely client-side until both entries match, avoiding unnecessary server
// round-trips for PIN mismatch errors.
function bindPinScreen(form) {
  const hiddenInput = form.querySelector('input[name="pin"]');
  const dots = qsa(".pin-dot", form);
  const help = form.querySelector(".pin-help");
  const mode = form.dataset.authMode;
  let pinValue = "";
  let firstPass = "";

  function syncDisplay() {
    // I keep the actual PIN value only in memory and in the hidden field. The visible dots are
    // purely a display layer so the keypad UI stays simple without exposing the entered digits.
    hiddenInput.value = pinValue;
    dots.forEach((dot, index) => {
      dot.classList.toggle("is-filled", index < pinValue.length);
    });
  }

  function resetPin() {
    pinValue = "";
    syncDisplay();
  }

  function appendDigit(digit) {
    if (pinValue.length >= dots.length) {
      return;
    }
    pinValue += digit;
    syncDisplay();
  }

  function backspace() {
    pinValue = pinValue.slice(0, -1);
    syncDisplay();
  }

  qsa("[data-pin-digit]", form).forEach((button) => {
    button.addEventListener("click", () =>
      appendDigit(button.dataset.pinDigit),
    );
  });

  const clearButton = form.querySelector("[data-pin-clear]");
  if (clearButton) {
    clearButton.addEventListener("click", () => {
      // In setup mode, clearing during the confirmation pass resets both the
      // current entry and the stored first pass so the user starts fresh.
      if (mode === "setup" && firstPass) {
        firstPass = "";
        if (help) {
          help.textContent =
            "Enter your new PIN, then confirm it on the next pass.";
        }
      }
      resetPin();
      setStatus(form, "");
    });
  }

  const backspaceButton = form.querySelector("[data-pin-backspace]");
  if (backspaceButton) {
    backspaceButton.addEventListener("click", () => {
      backspace();
      setStatus(form, "");
    });
  }

  // Keyboard support: digit keys append, Backspace removes the last digit,
  // Escape clears the entire entry, and Enter submits the form when at least
  // 4 digits have been entered.
  document.addEventListener("keydown", (event) => {
    if (!form.closest("body")) {
      return;
    }

    if (event.key >= "0" && event.key <= "9") {
      appendDigit(event.key);
      setStatus(form, "");
      return;
    }

    if (event.key === "Backspace") {
      backspace();
      setStatus(form, "");
      return;
    }

    if (event.key === "Escape") {
      resetPin();
      setStatus(form, "");
      return;
    }

    if (event.key === "Enter" && pinValue.length >= 4) {
      event.preventDefault();
      form.requestSubmit();
    }
  });

  form.addEventListener("submit", (event) => {
    if (pinValue.length < 4) {
      event.preventDefault();
      setStatus(form, "PIN must be at least 4 digits.", "error");
      restoreLoadingFormState(form);
      stopPageLoading();
      return;
    }

    // Non-setup mode (unlock, change PIN): let the form submit normally with the
    // PIN value in the hidden input. The server handles validation.
    if (mode !== "setup") {
      return;
    }

    // Setup mode: two-pass confirmation flow handled client-side.
    if (!firstPass) {
      // I handle first-run PIN confirmation locally instead of posting two fields to the server.
      // That keeps the screen compact and prevents mismatched attempts from touching backend state.
      firstPass = pinValue;
      resetPin();
      if (help) {
        help.textContent = "Re-enter the same PIN to confirm it.";
      }
      event.preventDefault();
      setStatus(form, "Confirm your new PIN.", "info");
      restoreLoadingFormState(form);
      stopPageLoading();
      return;
    }

    if (pinValue !== firstPass) {
      event.preventDefault();
      firstPass = "";
      resetPin();
      if (help) {
        help.textContent =
          "Enter your new PIN, then confirm it on the next pass.";
      }
      setStatus(form, "PIN entries did not match. Start again.", "error");
      restoreLoadingFormState(form);
      stopPageLoading();
      return;
    }
  });

  syncDisplay();
}

// initPinScreens finds all PIN entry forms on the page and binds the interactive
// keypad behaviour to each one via bindPinScreen.
function initPinScreens() {
  qsa("form[data-pin-screen]").forEach(bindPinScreen);
}

// initStatementPrinting turns each report's print control into a native print/save-PDF
// action. Keeping the trigger in the embedded UI gives non-technical operators a complete
// financial-statement handoff without asking them to open a browser menu or terminal.
function initStatementPrinting() {
  qsa("[data-print-page]").forEach((button) => {
    button.addEventListener("click", () => window.print());
  });
}

// The DOMContentLoaded handler kicks off all initialisation functions when the page
// is ready. Each init function is independent and self-contained—they query the DOM
// for their relevant elements and set up event listeners. Functions that find no
// matching elements simply return early with no side effects, so it is safe to call
// all of them on every page regardless of which features that page actually uses.
document.addEventListener("DOMContentLoaded", () => {
  setPageReady();
  initAutoDismissAlerts();
  initMoneyTooltips();
  initNavigationLoading();
  initPinScreens();
  initStatementPrinting();
  initExportForms();
  initRestoreForms();
  initBackupDownloads();
  initConfirmSubmits();
  initLoadingForms();
  initTableStages();
  initUpdateCenter();
  initCategoryEditor();
  initModals();
});
