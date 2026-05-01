package handlers

// I keep the chart payload intentionally small and serializable because charts are rendered on the
// frontend canvas layer, while the backend only needs to describe labels and series values.
// ChartData is the top-level structure passed to the frontend charting library (likely Chart.js
// or a similar canvas-based renderer). It carries an ordered list of axis labels and one or more
// datasets that map to visual series on the chart. By standardising on this shape across all
// report handlers, the Go templates can render a single, reusable chart component that works
// for income-vs-expenditure bars, balance sheet comparisons, and any future chart types without
// needing page-specific JavaScript or template logic.
type ChartData struct {
	Labels   []string       `json:"labels"`
	Datasets []ChartDataset `json:"datasets"`
}

// I model both bar and line series with one dataset shape so report handlers can assemble charts
// declaratively without page-specific chart structs.
// ChartDataset describes a single visual series on a chart—for example, "2026" as green bars or
// "2025" as grey bars. The Label field provides the legend text, Values holds the data points
// in the same order as the parent ChartData.Labels, and Type controls the rendering style
// ("bar" or "line"). Color is the primary fill or stroke colour for the series, while SoftColor
// is an optional lower-opacity variant used for hover effects, backgrounds, or area fills.
// Keeping both colour fields in the backend payload lets the frontend apply them directly
// without hardcoding colour logic in JavaScript.
type ChartDataset struct {
	Label     string    `json:"label"`
	Values    []float64 `json:"values"`
	Type      string    `json:"type"`
	Color     string    `json:"color"`
	SoftColor string    `json:"softColor,omitempty"`
}
