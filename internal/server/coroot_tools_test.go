package server

import (
	"encoding/json"
	"math/rand"
	"testing"
	"testing/quick"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

// ---------- formatServiceOverviewForCard ----------

func TestFormatServiceOverviewForCard_Basic(t *testing.T) {
	result := &coroot.ServiceOverviewResult{
		ID:     "svc-001",
		Name:   "api-gateway",
		Status: "warning",
		Summary: map[string]any{
			"latency_p99":  "120ms",
			"error_rate":   "2.3%",
			"request_rate": "1.2k/s",
		},
	}

	card := formatServiceOverviewForCard(result)

	if card["uiKind"] != "readonly_summary" {
		t.Errorf("uiKind = %v, want readonly_summary", card["uiKind"])
	}
	if card["title"] != "api-gateway 服务概览" {
		t.Errorf("title = %v, want 'api-gateway 服务概览'", card["title"])
	}
	if card["status"] != "warning" {
		t.Errorf("status = %v, want warning", card["status"])
	}

	rows, ok := card["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows is not []map[string]any")
	}
	// Should have 2 fixed rows (ID, status) + 3 summary rows = 5
	if len(rows) < 5 {
		t.Errorf("rows length = %d, want >= 5", len(rows))
	}

	// Verify the card is valid JSON
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
}

func TestFormatServiceOverviewForCard_NilFields(t *testing.T) {
	result := &coroot.ServiceOverviewResult{}

	card := formatServiceOverviewForCard(result)

	if card["uiKind"] != "readonly_summary" {
		t.Errorf("uiKind = %v, want readonly_summary", card["uiKind"])
	}
	if card["title"] != "N/A 服务概览" {
		t.Errorf("title = %v, want 'N/A 服务概览'", card["title"])
	}
	if card["status"] != "N/A" {
		t.Errorf("status = %v, want N/A", card["status"])
	}

	rows, ok := card["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows is not []map[string]any")
	}
	// 2 fixed rows (ID, status), no summary
	if len(rows) != 2 {
		t.Errorf("rows length = %d, want 2", len(rows))
	}
}

// ---------- formatMetricsForCard ----------

func TestFormatMetricsForCard_Basic(t *testing.T) {
	result := &coroot.MetricsResult{
		Metrics: []map[string]any{
			{
				"name": "cpu_usage",
				"values": []any{
					[]any{float64(1700000000), float64(45.2)},
					[]any{float64(1700000060), float64(47.1)},
				},
			},
		},
	}

	card := formatMetricsForCard(result)

	if card["uiKind"] != "readonly_chart" {
		t.Errorf("uiKind = %v, want readonly_chart", card["uiKind"])
	}
	if card["title"] != "指标趋势" {
		t.Errorf("title = %v, want '指标趋势'", card["title"])
	}

	visual, ok := card["visual"].(map[string]any)
	if !ok {
		t.Fatalf("visual is not map[string]any")
	}
	if visual["kind"] != "timeseries" {
		t.Errorf("visual.kind = %v, want timeseries", visual["kind"])
	}

	series, ok := visual["series"].([]map[string]any)
	if !ok {
		t.Fatalf("series is not []map[string]any")
	}
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if series[0]["name"] != "cpu_usage" {
		t.Errorf("series[0].name = %v, want cpu_usage", series[0]["name"])
	}

	data, ok := series[0]["data"].([]map[string]any)
	if !ok {
		t.Fatalf("data is not []map[string]any")
	}
	if len(data) != 2 {
		t.Errorf("data length = %d, want 2", len(data))
	}
}

func TestFormatMetricsForCard_Empty(t *testing.T) {
	result := &coroot.MetricsResult{}

	card := formatMetricsForCard(result)

	if card["uiKind"] != "readonly_chart" {
		t.Errorf("uiKind = %v, want readonly_chart", card["uiKind"])
	}

	visual := card["visual"].(map[string]any)
	series := visual["series"].([]map[string]any)
	if len(series) != 0 {
		t.Errorf("series length = %d, want 0", len(series))
	}
}

// ---------- formatAlertsForCard ----------

func TestFormatAlertsForCard_Basic(t *testing.T) {
	alerts := []coroot.Alert{
		{ID: "alert-1", Name: "High CPU", Severity: "Critical", Status: "firing"},
		{ID: "alert-2", Name: "Disk Full", Severity: "WARNING", Status: "resolved"},
	}

	card := formatAlertsForCard(alerts)

	if card["uiKind"] != "readonly_chart" {
		t.Errorf("uiKind = %v, want readonly_chart", card["uiKind"])
	}
	if card["title"] != "告警列表" {
		t.Errorf("title = %v, want '告警列表'", card["title"])
	}

	visual := card["visual"].(map[string]any)
	if visual["kind"] != "status_table" {
		t.Errorf("visual.kind = %v, want status_table", visual["kind"])
	}

	rows := visual["rows"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("rows length = %d, want 2", len(rows))
	}

	// Severity should be lowercased
	if rows[0]["status"] != "critical" {
		t.Errorf("rows[0].status = %v, want critical", rows[0]["status"])
	}
	if rows[1]["status"] != "warning" {
		t.Errorf("rows[1].status = %v, want warning", rows[1]["status"])
	}
}

func TestFormatAlertsForCard_Empty(t *testing.T) {
	card := formatAlertsForCard(nil)

	if card["uiKind"] != "readonly_chart" {
		t.Errorf("uiKind = %v, want readonly_chart", card["uiKind"])
	}

	visual := card["visual"].(map[string]any)
	rows := visual["rows"].([]map[string]any)
	if len(rows) != 0 {
		t.Errorf("rows length = %d, want 0", len(rows))
	}
}

func TestFormatAlertsForCard_MissingFields(t *testing.T) {
	alerts := []coroot.Alert{{}}

	card := formatAlertsForCard(alerts)
	visual := card["visual"].(map[string]any)
	rows := visual["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("rows length = %d, want 1", len(rows))
	}

	cells := rows[0]["cells"].([]string)
	for _, cell := range cells {
		if cell != "N/A" {
			// All fields should be "N/A" for empty alert
			t.Errorf("cell = %v, want N/A", cell)
		}
	}
}

// ---------- formatServicesForCard ----------

func TestFormatServicesForCard_Basic(t *testing.T) {
	services := []coroot.Service{
		{ID: "svc-1", Name: "api", Status: "ok"},
		{ID: "svc-2", Name: "web", Status: "healthy"},
		{ID: "svc-3", Name: "db", Status: "warning"},
		{ID: "svc-4", Name: "cache", Status: "critical"},
		{ID: "svc-5", Name: "queue", Status: "error"},
	}

	card := formatServicesForCard(services)

	if card["uiKind"] != "readonly_summary" {
		t.Errorf("uiKind = %v, want readonly_summary", card["uiKind"])
	}
	if card["title"] != "服务健康概览" {
		t.Errorf("title = %v, want '服务健康概览'", card["title"])
	}

	kpis := card["kpis"].([]map[string]any)
	if len(kpis) != 4 {
		t.Fatalf("kpis length = %d, want 4", len(kpis))
	}

	// Total
	if kpis[0]["value"] != 5 {
		t.Errorf("total = %v, want 5", kpis[0]["value"])
	}
	// Healthy: ok + healthy = 2
	if kpis[1]["value"] != 2 {
		t.Errorf("healthy = %v, want 2", kpis[1]["value"])
	}
	// Warning: 1
	if kpis[2]["value"] != 1 {
		t.Errorf("warning = %v, want 1", kpis[2]["value"])
	}
	// Critical: critical + error = 2
	if kpis[3]["value"] != 2 {
		t.Errorf("critical = %v, want 2", kpis[3]["value"])
	}

	visual := card["visual"].(map[string]any)
	if visual["kind"] != "kpi_strip" {
		t.Errorf("visual.kind = %v, want kpi_strip", visual["kind"])
	}
}

func TestFormatServicesForCard_Empty(t *testing.T) {
	card := formatServicesForCard(nil)

	kpis := card["kpis"].([]map[string]any)
	if kpis[0]["value"] != 0 {
		t.Errorf("total = %v, want 0", kpis[0]["value"])
	}
}

func TestFormatServicesForCard_KpiSumEqualsTotal(t *testing.T) {
	services := []coroot.Service{
		{Status: "ok"},
		{Status: "warning"},
		{Status: "critical"},
		{Status: "unknown_status"},
	}

	card := formatServicesForCard(services)
	kpis := card["kpis"].([]map[string]any)

	total := kpis[0]["value"].(int)
	healthy := kpis[1]["value"].(int)
	warning := kpis[2]["value"].(int)
	critical := kpis[3]["value"].(int)

	if healthy+warning+critical != total {
		t.Errorf("healthy(%d) + warning(%d) + critical(%d) = %d, want %d",
			healthy, warning, critical, healthy+warning+critical, total)
	}
}

// ---------- JSON roundtrip ----------

func TestAllFormatFunctions_ValidJSON(t *testing.T) {
	cards := []map[string]any{
		formatServiceOverviewForCard(&coroot.ServiceOverviewResult{
			ID: "x", Name: "y", Status: "ok",
			Summary: map[string]any{"k": "v"},
		}),
		formatMetricsForCard(&coroot.MetricsResult{
			Metrics: []map[string]any{{"name": "m", "values": []any{[]any{1.0, 2.0}}}},
		}),
		formatAlertsForCard([]coroot.Alert{{ID: "a", Name: "b", Severity: "info", Status: "firing"}}),
		formatServicesForCard([]coroot.Service{{ID: "s", Name: "n", Status: "ok"}}),
	}

	for i, card := range cards {
		data, err := json.Marshal(card)
		if err != nil {
			t.Errorf("card[%d] json.Marshal failed: %v", i, err)
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("card[%d] json.Unmarshal failed: %v", i, err)
			continue
		}
		if _, ok := parsed["uiKind"]; !ok {
			t.Errorf("card[%d] missing uiKind", i)
		}
		if _, ok := parsed["title"]; !ok {
			t.Errorf("card[%d] missing title", i)
		}
	}
}

// ---------- Property-Based Tests (testing/quick) ----------
// Feature: coroot-monitor-embed, Property 7: 后端工具响应格式化一致性
// **Validates: Requirements 8.4**

// quickConfig returns a quick.Config with at least 100 iterations.
func quickConfig() *quick.Config {
	return &quick.Config{MaxCount: 100}
}

// ---------- Generators ----------

// randString generates a random string of length 0..maxLen from the given rng.
func randString(r *rand.Rand, maxLen int) string {
	n := r.Intn(maxLen + 1)
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte('a' + r.Intn(26))
	}
	return string(buf)
}

// genServiceOverviewResult generates a random ServiceOverviewResult.
func genServiceOverviewResult(r *rand.Rand) *coroot.ServiceOverviewResult {
	result := &coroot.ServiceOverviewResult{
		ID:     randString(r, 20),
		Name:   randString(r, 30),
		Status: randString(r, 10),
	}
	// Optionally add summary fields.
	nSummary := r.Intn(6)
	if nSummary > 0 {
		result.Summary = make(map[string]any, nSummary)
		for i := 0; i < nSummary; i++ {
			key := randString(r, 15)
			if key == "" {
				key = "k"
			}
			result.Summary[key] = randString(r, 20)
		}
	}
	return result
}

// genMetricsResult generates a random MetricsResult.
func genMetricsResult(r *rand.Rand) *coroot.MetricsResult {
	nMetrics := r.Intn(5)
	metrics := make([]map[string]any, nMetrics)
	for i := 0; i < nMetrics; i++ {
		m := map[string]any{
			"name": randString(r, 20),
		}
		nValues := r.Intn(10)
		values := make([]any, nValues)
		for j := 0; j < nValues; j++ {
			values[j] = []any{r.Float64() * 1e9, r.Float64() * 100}
		}
		m["values"] = values
		metrics[i] = m
	}
	return &coroot.MetricsResult{Metrics: metrics}
}

// genAlerts generates a random slice of Alert.
func genAlerts(r *rand.Rand) []coroot.Alert {
	n := r.Intn(10)
	alerts := make([]coroot.Alert, n)
	for i := 0; i < n; i++ {
		alerts[i] = coroot.Alert{
			ID:       randString(r, 15),
			Name:     randString(r, 20),
			Severity: randString(r, 10),
			Status:   randString(r, 10),
		}
	}
	return alerts
}

// genServices generates a random slice of Service.
func genServices(r *rand.Rand) []coroot.Service {
	statuses := []string{"ok", "healthy", "warning", "critical", "error", "unknown", ""}
	n := r.Intn(20)
	services := make([]coroot.Service, n)
	for i := 0; i < n; i++ {
		services[i] = coroot.Service{
			ID:     randString(r, 15),
			Name:   randString(r, 20),
			Status: statuses[r.Intn(len(statuses))],
		}
	}
	return services
}

// ---------- Property tests ----------

// assertCardHasUiKindAndTitle checks that a card map contains "uiKind" and "title"
// keys and that the card can be marshaled/unmarshaled as valid JSON.
func assertCardHasUiKindAndTitle(t *testing.T, card map[string]any, label string) {
	t.Helper()
	if _, ok := card["uiKind"]; !ok {
		t.Errorf("%s: missing uiKind key", label)
	}
	if _, ok := card["title"]; !ok {
		t.Errorf("%s: missing title key", label)
	}
	data, err := json.Marshal(card)
	if err != nil {
		t.Errorf("%s: json.Marshal failed: %v", label, err)
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("%s: json.Unmarshal roundtrip failed: %v", label, err)
	}
}

func TestProperty7_FormatServiceOverviewForCard(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		result := genServiceOverviewResult(r)
		card := formatServiceOverviewForCard(result)

		// Must contain uiKind and title.
		if _, ok := card["uiKind"]; !ok {
			return false
		}
		if _, ok := card["title"]; !ok {
			return false
		}
		// Must be valid JSON roundtrip.
		data, err := json.Marshal(card)
		if err != nil {
			return false
		}
		var parsed map[string]any
		return json.Unmarshal(data, &parsed) == nil
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 (formatServiceOverviewForCard): %v", err)
	}
}

func TestProperty7_FormatMetricsForCard(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		result := genMetricsResult(r)
		card := formatMetricsForCard(result)

		if _, ok := card["uiKind"]; !ok {
			return false
		}
		if _, ok := card["title"]; !ok {
			return false
		}
		data, err := json.Marshal(card)
		if err != nil {
			return false
		}
		var parsed map[string]any
		return json.Unmarshal(data, &parsed) == nil
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 (formatMetricsForCard): %v", err)
	}
}

func TestProperty7_FormatAlertsForCard(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		alerts := genAlerts(r)
		card := formatAlertsForCard(alerts)

		if _, ok := card["uiKind"]; !ok {
			return false
		}
		if _, ok := card["title"]; !ok {
			return false
		}
		data, err := json.Marshal(card)
		if err != nil {
			return false
		}
		var parsed map[string]any
		return json.Unmarshal(data, &parsed) == nil
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 (formatAlertsForCard): %v", err)
	}
}

func TestProperty7_FormatServicesForCard(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		services := genServices(r)
		card := formatServicesForCard(services)

		if _, ok := card["uiKind"]; !ok {
			return false
		}
		if _, ok := card["title"]; !ok {
			return false
		}
		data, err := json.Marshal(card)
		if err != nil {
			return false
		}
		var parsed map[string]any
		return json.Unmarshal(data, &parsed) == nil
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 (formatServicesForCard): %v", err)
	}
}
