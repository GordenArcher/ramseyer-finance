// CHART_COLORS centralises the visual palette for every canvas-drawn chart in the
// application. Grid lines use a barely-visible dark stroke so they guide the eye
// without competing with the data. Axis labels are a muted slate for readability.
// Data-point markers use an off-white fill (`#fffdf8`) that contrasts against every
// bar and line colour without looking like pure white. The band colour is the
// application's gold accent at very low opacity—it is reserved for hover highlights
// or future interactive overlays and is defined here so it stays consistent with the
// Go-side ChartDataset.SoftColor values.
const CHART_COLORS = {
  grid: "rgba(29, 36, 51, 0.12)",
  axis: "rgba(88, 97, 116, 0.92)",
  marker: "#fffdf8",
  band: "rgba(186, 122, 17, 0.08)",
};

// queryAll is a convenience wrapper around querySelectorAll that converts the live
// NodeList into a plain Array. This lets the chart code use forEach, filter, map, and
// find without sprinkling `Array.from()` at every call site. The optional `root`
// parameter scopes the query to a specific container element rather than the whole
// document, which is useful when the chart module needs to search within a single
// chart shell.
function queryAll(selector, root = document) {
  return Array.from(root.querySelectorAll(selector));
}

// readChartData extracts the serialised chart configuration from a hidden DOM element
// whose `id` matches `sourceId`. The Go backend embeds chart data as JSON inside a
// `<script>` tag (or similar container) so the canvas renderer can stay entirely
// data-driven. If the element is absent or contains malformed JSON the function
// returns null—callers must handle the null case gracefully rather than crashing.
function readChartData(sourceId) {
  const node = document.getElementById(sourceId);
  if (!node) {
    return null;
  }

  try {
    return JSON.parse(node.textContent || "null");
  } catch {
    return null;
  }
}

// formatAxisValue produces a compact Y-axis label from a numeric data value. Values
// ≥ 1 billion are shown with a "B" suffix, ≥ 1 million with "M", and ≥ 1 thousand
// with "K"—each to one decimal place. Values below 1,000 are rounded to the nearest
// integer. Absolute value is used for the threshold check so negative numbers receive
// the same unit treatment as their positive counterparts (e.g., -1.5M). This keeps
// the vertical axis legible even when the chart spans several orders of magnitude.
function formatAxisValue(value) {
  const absolute = Math.abs(value);
  if (absolute >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(1)}B`;
  }
  if (absolute >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(1)}M`;
  }
  if (absolute >= 1_000) {
    return `${(value / 1_000).toFixed(1)}K`;
  }
  return `${Math.round(value)}`;
}

// formatTooltipValue formats a number for the hover tooltip using the browser's
// Intl.NumberFormat with en-US locale, comma grouping, and up to two decimal places.
// This is intentionally more precise than formatAxisValue because the tooltip is
// meant for detailed inspection of a single data point, whereas axis labels only
// need to communicate approximate scale.
function formatTooltipValue(value) {
  return new Intl.NumberFormat("en-US", {
    maximumFractionDigits: 2,
  }).format(value);
}

// ensureChartTooltip returns the floating tooltip element inside a chart shell,
// creating it if it does not already exist. A single tooltip node is reused for the
// lifetime of the chart rather than being created and destroyed on every hover event.
// The tooltip is absolutely positioned relative to the shell by showTooltip.
function ensureChartTooltip(shell) {
  let tooltip = shell.querySelector(".chart-tooltip");
  if (tooltip) {
    return tooltip;
  }

  tooltip = document.createElement("div");
  tooltip.className = "chart-tooltip";
  shell.appendChild(tooltip);
  return tooltip;
}

// hideTooltip removes the `is-visible` class from the tooltip element, which triggers
// a CSS opacity transition that fades the tooltip out. The element stays in the DOM
// so it can be shown again without re-creation.
function hideTooltip(tooltip) {
  tooltip.classList.remove("is-visible");
}

// showTooltip populates the tooltip with the provided HTML lines, makes it visible,
// and positions it near the mouse cursor within the chart shell. The tooltip is
// offset 16px to the right and 16px above the cursor so it does not obscure the data
// point the user is hovering over. Horizontal clamping ensures the tooltip never
// overflows the shell's right edge; a 12px minimum left margin prevents it from
// touching the shell border or the browser edge.
function showTooltip(tooltip, shell, event, lines) {
  tooltip.innerHTML = lines.join("");
  tooltip.classList.add("is-visible");

  const shellRect = shell.getBoundingClientRect();
  const tooltipRect = tooltip.getBoundingClientRect();
  const offsetX = event.clientX - shellRect.left + 16;
  const offsetY = event.clientY - shellRect.top - tooltipRect.height - 16;
  const maxLeft = shellRect.width - tooltipRect.width - 12;

  tooltip.style.left = `${Math.max(12, Math.min(offsetX, maxLeft))}px`;
  tooltip.style.top = `${Math.max(12, offsetY)}px`;
}

// buildChartModel normalises the raw chart data payload into a consistent { labels,
// datasets } shape. Because the data may come from a Go json.Marshal (which produces
// PascalCase keys like "Labels" and "Datasets") or from a hand-rolled JavaScript
// object (camelCase), this function checks both casing variants. The normalised model
// is what the rest of the chart renderer consumes, so no other function needs to
// worry about the original key capitalisation.
function buildChartModel(chartData) {
  // I accept both camelCase and PascalCase payloads because the chart data originates from Go
  // structs and template JSON output. Normalizing once here keeps the renderer itself simple.
  const labels = chartData.labels || chartData.Labels || [];
  const datasets = Array.isArray(chartData.datasets) ? chartData.datasets : [];
  return { labels, datasets };
}

// getCanvasSize calculates the physical pixel dimensions for a canvas element,
// factoring in the device pixel ratio so charts render sharply on Retina and HiDPI
// displays. The CSS layout size (from getBoundingClientRect) is multiplied by the
// DPR to determine the backing buffer size. Minimum dimensions of 320×240 prevent
// the chart from collapsing into an unreadably small rectangle on narrow viewports.
function getCanvasSize(canvas) {
  const dpr = window.devicePixelRatio || 1;
  const bounds = canvas.getBoundingClientRect();
  return {
    dpr,
    width: Math.max(320, Math.round(bounds.width * dpr)),
    height: Math.max(240, Math.round(bounds.height * dpr)),
  };
}

// drawChart is the stateless chart renderer. It reads the chart model from the DOM,
// sizes the canvas backing buffer, computes a single shared Y-axis scale across all
// bar and line datasets (so surplus lines are visually comparable to income and
// expenditure bars), draws grid lines, axis labels, bar segments with a two-tone fill
// (soft background plus solid foreground at 80% height), line paths with circle
// markers, and wires up mouse hover for the tooltip. The `progress` parameter (0–1)
// controls the bar and line animation height; callers pass 1 for an immediate full
// draw (e.g. on resize) or an eased value during the initial reveal animation.
// Because the charts are small (typically 12 data points), the entire chart is
// redrawn from source data on every frame rather than maintaining a persistent chart
// object—this keeps the code simpler and avoids state synchronisation bugs.
function drawChart(canvas, progress = 1) {
  const shell = canvas?.closest(".chart-shell");
  const sourceId = canvas?.dataset.chartSource;
  const chartData = sourceId ? readChartData(sourceId) : null;
  const model = chartData ? buildChartModel(chartData) : null;
  if (!canvas || !shell || !model || !Array.isArray(model.labels)) {
    return;
  }

  // I redraw directly from source data on each pass instead of maintaining a separate chart
  // instance model. These charts are small enough that the simpler approach is easier to keep
  // correct across resizes, animations, and template-driven page loads.
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    return;
  }

  // I compute the scale across every dataset so mixed bar and line series share one honest axis.
  // That keeps the surplus line visually comparable to the income and expenditure bars.
  const size = getCanvasSize(canvas);
  if (canvas.width !== size.width || canvas.height !== size.height) {
    canvas.width = size.width;
    canvas.height = size.height;
  }

  const width = canvas.width;
  const height = canvas.height;
  // Padding reserves space for Y-axis labels (left), the title/legend area (top),
  // X-axis labels (bottom), and a small right margin for visual balance. These
  // values are tuned for the 12px Avenir Next font used throughout the UI.
  const padding = { top: 34, right: 28, bottom: 44, left: 70 };
  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;
  const labels = model.labels;
  const datasets = model.datasets;
  // Separate datasets into bars and lines because they use different rendering
  // paths: bars are grouped side-by-side per label, lines are continuous paths
  // with circle markers. Filtering by `type !== "line"` means any dataset without
  // an explicit type is treated as a bar (the safe default).
  const barDatasets = datasets.filter((set) => set.type !== "line");
  const lineDatasets = datasets.filter((set) => set.type === "line");
  // Flatten all dataset values into one array so the Y-axis scale encompasses
  // every visible data point, ensuring bars and lines share the same reference
  // frame. Empty datasets contribute no values and do not affect the scale.
  const allValues = datasets.flatMap((set) => set.values || []);
  const maxSeriesValue = Math.max(0, ...allValues);
  const minSeriesValue = Math.min(0, ...allValues);
  // Add 12% padding above the maximum value so the tallest bar does not touch the
  // top edge of the plot area. A smaller 4.2% padding (35% of the top padding) is
  // added below the minimum for negative values, which are less common.
  const rangePadding = Math.max(1, (maxSeriesValue - minSeriesValue) * 0.12);
  const maxValue = maxSeriesValue + rangePadding;
  const minValue = minSeriesValue - rangePadding * 0.35;
  const valueRange = Math.max(1, maxValue - minValue);
  const tickCount = 5;
  const tooltip = ensureChartTooltip(shell);

  ctx.clearRect(0, 0, width, height);

  // I keep the chart background flat so it matches the rest of the UI after gradients were removed.
  ctx.fillStyle = "rgba(255, 255, 255, 0.94)";
  ctx.fillRect(0, 0, width, height);

  // yForValue maps a data value to a Y pixel coordinate. The Y-axis is inverted:
  // larger data values produce smaller pixel Y values (higher on screen). The
  // formula normalises the value within the [minValue, maxValue] range and scales
  // it to the plot height.
  const yForValue = (value) =>
    padding.top + ((maxValue - value) / valueRange) * plotHeight;
  const zeroY = yForValue(0);

  // Horizontal grid lines at each tick mark. The grid uses a very subtle stroke
  // (12% opacity on a dark base) so it guides the eye across the chart without
  // visually competing with the bars and lines.
  ctx.strokeStyle = CHART_COLORS.grid;
  ctx.lineWidth = 1;
  for (let index = 0; index <= tickCount; index += 1) {
    const y = padding.top + (plotHeight / tickCount) * index;
    ctx.beginPath();
    ctx.moveTo(padding.left, y);
    ctx.lineTo(width - padding.right, y);
    ctx.stroke();
  }

  // The zero baseline is drawn with a thicker, slightly darker stroke to anchor
  // the viewer's perception of positive versus negative values. It spans the full
  // plot width.
  ctx.beginPath();
  ctx.lineWidth = 1.6;
  ctx.strokeStyle = "rgba(29, 36, 51, 0.22)";
  ctx.moveTo(padding.left, zeroY);
  ctx.lineTo(width - padding.right, zeroY);
  ctx.stroke();

  // Y-axis value labels are drawn to the left of the grid, right-aligned against
  // the imaginary axis line. The font matches the application's body text stack.
  // Labels are vertically centred on their corresponding grid line.
  ctx.fillStyle = CHART_COLORS.axis;
  ctx.font = "12px 'Avenir Next', sans-serif";
  ctx.textAlign = "right";
  ctx.textBaseline = "middle";
  for (let index = 0; index <= tickCount; index += 1) {
    const value = maxValue - (valueRange / tickCount) * index;
    const y = padding.top + (plotHeight / tickCount) * index;
    ctx.fillText(formatAxisValue(value), padding.left - 12, y);
  }

  // groupWidth is the horizontal space allocated to each X-axis label. barSlotWidth
  // caps the total width reserved for all bar datasets within a group—it is capped
  // at 72% of groupWidth and at 20px per dataset so bars don't become comically
  // wide when there are very few labels. barGap adds spacing between side-by-side
  // bars of different datasets. barWidth is the computed width of each individual
  // bar, with a 12px minimum to keep narrow bars clickable/tappable.
  const groupWidth = plotWidth / Math.max(labels.length, 1);
  const barSlotWidth =
    barDatasets.length > 0
      ? Math.min(groupWidth * 0.72, 20 * barDatasets.length)
      : 0;
  const barGap = barDatasets.length > 1 ? 8 : 0;
  const barWidth =
    barDatasets.length > 0
      ? Math.max(
          12,
          (barSlotWidth - barGap * (barDatasets.length - 1)) /
            barDatasets.length,
        )
      : 0;
  // hoverPoints accumulates the interactive hit areas and data values for tooltip
  // display. Each entry corresponds to one X-axis label and records the horizontal
  // band where mouse events should trigger the tooltip, plus the data values from
  // all datasets at that index.
  const hoverPoints = [];

  labels.forEach((label, index) => {
    const centerX = padding.left + groupWidth * index + groupWidth / 2;
    // The hover band is slightly narrower than the full group width (4px inset on
    // each side) so that moving the mouse between adjacent bars doesn't flicker
    // between two tooltips.
    const bandLeft = centerX - groupWidth / 2 + 4;
    const bandWidth = groupWidth - 8;

    // X-axis label centred under the bar group, positioned just below the plot
    // area with 14px of separation.
    ctx.fillStyle = CHART_COLORS.axis;
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    ctx.fillText(label, centerX, height - padding.bottom + 14);

    const point = { index, label, x: centerX, bandLeft, bandWidth, values: [] };

    // Each bar dataset draws two overlapping rectangles at this label position:
    // a full-height background fill using the dataset's softColor (low-opacity
    // variant), and a foreground fill at 80% height using the solid colour. This
    // two-tone treatment gives bars a subtle depth without relying on gradients.
    // The animated height is scaled by the progress parameter, so during the
    // initial reveal bars grow from zero to their full height.
    barDatasets.forEach((dataset, datasetIndex) => {
      const rawValue = dataset.values?.[index] || 0;
      const animatedValue = rawValue * progress;
      const valueHeight = Math.abs((animatedValue / valueRange) * plotHeight);
      const x = centerX - barSlotWidth / 2 + datasetIndex * (barWidth + barGap);
      const y = rawValue >= 0 ? zeroY - valueHeight : zeroY;

      ctx.fillStyle = dataset.softColor || "rgba(29, 36, 51, 0.12)";
      ctx.fillRect(x, y, barWidth, valueHeight);
      ctx.fillStyle = dataset.color || "#586174";
      ctx.fillRect(x, y, barWidth, valueHeight * 0.8);

      point.values.push({
        label: dataset.label,
        value: rawValue,
        color: dataset.color || "#586174",
      });
    });

    hoverPoints.push(point);
  });

  // Line datasets are drawn after bars so they render on top. Each line is a
  // single continuous path: moveTo the first point, lineTo each subsequent point.
  // Circle markers (radius 4.5px) are drawn at every data point with a white fill
  // and a coloured stroke matching the line colour, making individual values
  // identifiable even where the line overlaps bars.
  lineDatasets.forEach((dataset) => {
    ctx.beginPath();
    ctx.lineWidth = 3;
    ctx.strokeStyle = dataset.color || "#b44432";
    dataset.values?.forEach((value, index) => {
      const centerX = padding.left + groupWidth * index + groupWidth / 2;
      const animatedValue = value * progress;
      const y = yForValue(animatedValue);
      if (index === 0) {
        ctx.moveTo(centerX, y);
      } else {
        ctx.lineTo(centerX, y);
      }
    });
    ctx.stroke();

    dataset.values?.forEach((value, index) => {
      const centerX = padding.left + groupWidth * index + groupWidth / 2;
      const animatedValue = value * progress;
      const y = yForValue(animatedValue);
      ctx.beginPath();
      ctx.fillStyle = CHART_COLORS.marker;
      ctx.strokeStyle = dataset.color || "#b44432";
      ctx.lineWidth = 2;
      ctx.arc(centerX, y, 4.5, 0, Math.PI * 2);
      ctx.fill();
      ctx.stroke();
      hoverPoints[index].lineY = y;
      hoverPoints[index].values.push({
        label: dataset.label,
        value,
        color: dataset.color || "#b44432",
      });
    });
  });

  // Mouse move handler: convert the cursor's client coordinates to canvas pixel
  // coordinates (accounting for CSS-to-buffer scaling), check whether the cursor
  // is inside the plot area and over a valid hover band, and show or hide the
  // tooltip accordingly. The tooltip displays the X-axis label and all dataset
  // values at that point, each prefixed with a colour-coded dot.
  canvas.onmousemove = (event) => {
    const bounds = canvas.getBoundingClientRect();
    const scaleX = canvas.width / bounds.width;
    const scaleY = canvas.height / bounds.height;
    const mouseX = (event.clientX - bounds.left) * scaleX;
    const mouseY = (event.clientY - bounds.top) * scaleY;
    // Ignore mouse positions outside the plot area (in the padding regions) to
    // avoid showing a tooltip when the cursor is over axis labels or whitespace.
    if (
      mouseX < padding.left ||
      mouseX > width - padding.right ||
      mouseY < padding.top ||
      mouseY > height - padding.bottom
    ) {
      hideTooltip(tooltip);
      return;
    }

    const activePoint = hoverPoints.find(
      (point) =>
        mouseX >= point.bandLeft && mouseX <= point.bandLeft + point.bandWidth,
    );

    if (!activePoint) {
      hideTooltip(tooltip);
      return;
    }

    showTooltip(tooltip, shell, event, [
      `<strong>${activePoint.label}</strong>`,
      ...activePoint.values.map(
        (entry) =>
          `<span><i class="chart-tooltip-dot" style="background:${entry.color}"></i>${entry.label}: ${formatTooltipValue(entry.value || 0)}</span>`,
      ),
    ]);
  };

  // Hide the tooltip when the cursor leaves the canvas entirely.
  canvas.onmouseleave = () => {
    hideTooltip(tooltip);
  };
}

// animateChart runs the initial chart reveal animation. It uses requestAnimationFrame
// to call drawChart with an eased progress value over a 760ms duration. The easing
// function is a quadratic ease-out (1 - (1-t)²), which starts quickly and decelerates
// smoothly, giving the bars a natural "spring into place" feel. When progress reaches
// 1 the animation stops—no explicit cancel is needed because requestAnimationFrame
// simply won't schedule another frame.
function animateChart(canvas) {
  const start = performance.now();
  const duration = 760;

  function frame(now) {
    const progress = Math.min(1, (now - start) / duration);
    const eased = 1 - (1 - progress) * (1 - progress);
    drawChart(canvas, eased);
    if (progress < 1) {
      window.requestAnimationFrame(frame);
    }
  }

  window.requestAnimationFrame(frame);
}

// initCharts finds every canvas element tagged with `data-canvas-chart`, starts the
// reveal animation for each one, and registers a debounced window resize handler that
// redraws all charts at full progress (no animation) 120ms after the user stops
// resizing. The debounce prevents dozens of expensive redraws during a single
// window-drag operation. If no chart canvases exist on the page the function returns
// immediately without attaching any listeners, keeping the module inert on pages
// that don't use charts.
function initCharts() {
  const canvases = queryAll("[data-canvas-chart]");
  if (canvases.length === 0) {
    return;
  }

  canvases.forEach((canvas) => animateChart(canvas));

  let resizeTimer = 0;
  window.addEventListener("resize", () => {
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => {
      canvases.forEach((canvas) => drawChart(canvas, 1));
    }, 120);
  });
}

// Boot the chart module when the DOM is fully parsed. This listener is separate from
// the main application init in app.js so the chart code can be loaded independently
// or deferred without creating a dependency on the main script's initialisation order.
document.addEventListener("DOMContentLoaded", () => {
  initCharts();
});
