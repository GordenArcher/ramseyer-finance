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

// initAutoDismissAlerts finds all alert elements marked with the data-auto-dismiss-alert
// attribute and schedules them to fade out and be removed from the DOM after 3 seconds.
// The removal uses a two-stage animation: first the "is-dismissing" class triggers a CSS
// fade-out transition (220ms), and then the element is removed via remove() after the
// transition completes. This avoids a jarring instant disappearance.
function initAutoDismissAlerts() {
  qsa("[data-auto-dismiss-alert]").forEach((alert) => {
    window.setTimeout(() => {
      alert.classList.add("is-dismissing");
      window.setTimeout(() => {
        alert.remove();
      }, 220);
    }, 3000);
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

// initRestoreForms sets up the backup restore form with dual input modes: a native
// desktop file picker (via the Go-backed window.pickBackupFile function) and a
// standard browser <input type="file"> fallback. It wires up the native picker
// button to call the desktop shell, updates the selected-file label when either
// input changes, and validates on submit that at least one source has been selected
// before allowing the form to proceed. This dual-mode design ensures the restore
// feature works in both the desktop webview and a regular browser.
function initRestoreForms() {
  qsa("form[data-restore-form]").forEach((form) => {
    const nativePathInput = form.querySelector("[data-native-file-path]");
    const nativeNameNode = form.querySelector("[data-native-file-name]");
    const browserFileInput = form.querySelector("[data-restore-file-input]");
    const nativePickerButton = form.querySelector("[data-native-file-picker]");

    function clearStatus() {
      const statusNode = form.querySelector(".form-status");
      if (statusNode) {
        statusNode.textContent = "";
        statusNode.dataset.tone = "";
      }
    }

    function setSelectedLabel(label) {
      if (nativeNameNode) {
        nativeNameNode.textContent = label;
      }
    }

    // When the browser file input changes, clear the native path (so the two inputs
    // are mutually exclusive) and update the label to show the selected filename.
    if (browserFileInput) {
      browserFileInput.addEventListener("change", () => {
        clearStatus();
        if (browserFileInput.files && browserFileInput.files.length > 0) {
          if (nativePathInput) {
            nativePathInput.value = "";
          }
          setSelectedLabel(`Browser file: ${browserFileInput.files[0].name}`);
        } else if (!nativePathInput?.value) {
          setSelectedLabel("No backup file selected yet.");
        }
      });
    }

    // The native picker button calls the desktop shell's file picker (exposed as
    // window.pickBackupFile by the Go backend). If the function is not available
    // (e.g., in a regular browser), it shows a message telling the user to use the
    // browser file input instead. The button shows a loading state while the native
    // dialog is open, since the dialog is modal and may take time for the user to
    // navigate.
    if (nativePickerButton) {
      nativePickerButton.addEventListener("click", async () => {
        clearStatus();
        if (typeof window.pickBackupFile !== "function") {
          setSelectedLabel(
            "Native picker unavailable here. Use the browser fallback file input.",
          );
          return;
        }

        nativePickerButton.disabled = true;
        nativePickerButton.classList.add("is-loading");
        try {
          // I ask the desktop shell for the file path because the embedded webview does not
          // reliably surface file picker dialogs the way a normal browser does.
          const selectedPath = await window.pickBackupFile();
          if (!selectedPath) {
            return;
          }
          if (nativePathInput) {
            nativePathInput.value = selectedPath;
          }
          if (browserFileInput) {
            browserFileInput.value = "";
          }
          const parts = String(selectedPath).split(/[\\/]/);
          setSelectedLabel(`Desktop file: ${parts[parts.length - 1]}`);
        } catch (error) {
          setFormMessage(
            form,
            error instanceof Error
              ? error.message
              : "Could not open the desktop file picker.",
            "error",
          );
        } finally {
          nativePickerButton.disabled = false;
          nativePickerButton.classList.remove("is-loading");
        }
      });
    }

    // On form submit, validate that at least one restore source (native path or
    // browser file) has been provided. If neither is set, prevent submission and
    // show an error message immediately rather than sending an empty request to the
    // server. This gives faster feedback and avoids a pointless network round-trip.
    form.addEventListener("submit", (event) => {
      const hasNativePath = Boolean(nativePathInput?.value.trim());
      const hasBrowserFile = Boolean(
        browserFileInput?.files && browserFileInput.files.length > 0,
      );
      // I accept either source, but never allow a restore request with neither. The backend
      // checks again, but stopping here keeps the error immediate and prevents a fake loading state.
      if (hasNativePath || hasBrowserFile) {
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

// initFilePickers wires up custom-styled file input components (marked with
// data-file-picker). Each picker consists of a hidden <input type="file">, a visible
// trigger button that opens the file dialog, and a name display node that shows the
// selected filename or "No file chosen". Clicking the trigger programmatically clicks
// the hidden input, and the change event on the input updates the display.
function initFilePickers() {
  qsa("[data-file-picker]").forEach((picker) => {
    const input = picker.querySelector("[data-file-input]");
    const trigger = picker.querySelector("[data-file-trigger]");
    const nameNode = picker.querySelector("[data-file-name]");
    if (!input || !trigger || !nameNode) {
      return;
    }

    const syncName = () => {
      if (input.files && input.files.length > 0) {
        nameNode.textContent = input.files[0].name;
      } else {
        nameNode.textContent = "No file chosen";
      }
    };

    trigger.addEventListener("click", () => input.click());
    input.addEventListener("change", syncName);
    syncName();
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
  initExportForms();
  initRestoreForms();
  initBackupDownloads();
  initFilePickers();
  initConfirmSubmits();
  initLoadingForms();
  initTableStages();
  initModals();
});
