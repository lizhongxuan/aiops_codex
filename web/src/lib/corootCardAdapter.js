/**
 * Coroot Card Adapter
 *
 * Adapts raw Coroot API responses into MCP UI card payloads.
 * Each function handles missing fields with safe defaults ("N/A" for strings, 0 for numbers).
 */

/**
 * adaptServiceOverview(result) → McpSummaryCard
 * Converts a Coroot service overview result into a readonly_summary card.
 */
export function adaptServiceOverview(result) {
  try {
    const data = result && typeof result === "object" ? result : {};
    const summaryEntries = data.summary && typeof data.summary === "object" && !Array.isArray(data.summary)
      ? Object.entries(data.summary)
      : [];

    return {
      uiKind: "readonly_summary",
      title: `${data.name || "N/A"} 服务概览`,
      status: data.status || "N/A",
      rows: [
        { label: "服务 ID", value: data.id || "N/A" },
        { label: "状态", value: data.status || "N/A", highlight: true },
        ...summaryEntries.map(([k, v]) => ({ label: k, value: v ?? "N/A" })),
      ],
    };
  } catch {
    return {
      uiKind: "readonly_summary",
      title: "N/A 服务概览",
      status: "N/A",
      rows: [],
    };
  }
}

/**
 * adaptMetrics(result) → McpTimeseriesChartCard
 * Converts Coroot metrics data into a readonly_chart card with timeseries visual.
 */
export function adaptMetrics(result) {
  try {
    const data = result && typeof result === "object" ? result : {};
    const metrics = Array.isArray(data.metrics) ? data.metrics : [];

    return {
      uiKind: "readonly_chart",
      title: "指标趋势",
      visual: {
        kind: "timeseries",
        series: metrics.map((m) => {
          const metric = m && typeof m === "object" ? m : {};
          const values = Array.isArray(metric.values) ? metric.values : [];
          return {
            name: metric.name || "N/A",
            data: values.map((pair) => {
              const arr = Array.isArray(pair) ? pair : [];
              return { timestamp: arr[0] || 0, value: arr[1] || 0 };
            }),
          };
        }),
      },
    };
  } catch {
    return {
      uiKind: "readonly_chart",
      title: "指标趋势",
      visual: { kind: "timeseries", series: [] },
    };
  }
}

/**
 * adaptAlerts(alerts) → McpStatusTableCard
 * Converts Coroot alerts array into a readonly_chart card with status_table visual.
 */
export function adaptAlerts(alerts) {
  try {
    const list = Array.isArray(alerts) ? alerts : [];

    return {
      uiKind: "readonly_chart",
      title: "告警列表",
      visual: {
        kind: "status_table",
        columns: ["ID", "名称", "严重程度", "状态"],
        rows: list.map((a) => {
          const alert = a && typeof a === "object" ? a : {};
          return {
            cells: [
              alert.id || "N/A",
              alert.name || "N/A",
              (alert.severity || "N/A").toLowerCase(),
              alert.status || "N/A",
            ],
            status: (alert.severity || "N/A").toLowerCase(),
          };
        }),
      },
    };
  } catch {
    return {
      uiKind: "readonly_chart",
      title: "告警列表",
      visual: { kind: "status_table", columns: ["ID", "名称", "严重程度", "状态"], rows: [] },
    };
  }
}

/**
 * adaptServiceStats(services) → McpKpiStripCard
 * Aggregates service health statuses into a readonly_summary card with KPI strip visual.
 */
export function adaptServiceStats(services) {
  try {
    const list = Array.isArray(services) ? services : [];
    const total = list.length;
    let healthy = 0;
    let warning = 0;
    let critical = 0;

    for (const svc of list) {
      const status = (svc && typeof svc === "object" && typeof svc.status === "string"
        ? svc.status
        : ""
      ).toLowerCase();

      if (status === "ok" || status === "healthy") {
        healthy++;
      } else if (status === "warning") {
        warning++;
      } else {
        // "critical", "error", or any unknown status counts toward critical
        critical++;
      }
    }

    return {
      uiKind: "readonly_summary",
      title: "服务健康概览",
      kpis: [
        { label: "总服务数", value: total },
        { label: "健康", value: healthy, color: "green" },
        { label: "告警", value: warning, color: "amber" },
        { label: "异常", value: critical, color: "red" },
      ],
      visual: { kind: "kpi_strip" },
    };
  } catch {
    return {
      uiKind: "readonly_summary",
      title: "服务健康概览",
      kpis: [
        { label: "总服务数", value: 0 },
        { label: "健康", value: 0, color: "green" },
        { label: "告警", value: 0, color: "amber" },
        { label: "异常", value: 0, color: "red" },
      ],
      visual: { kind: "kpi_strip" },
    };
  }
}
