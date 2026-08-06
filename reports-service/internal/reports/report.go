package reports

import "time"

const dateLayout = "2006-01-02"

type Period struct {
	From             string `json:"from"`
	To               string `json:"to"`
	RequestedTo      string `json:"requestedTo"`
	DataCoveredUntil string `json:"dataCoveredUntil"`
	Truncated        bool   `json:"truncated"`
}

type Totals struct {
	Days               int     `json:"days"`
	ReadingsTotal      uint64  `json:"readingsTotal"`
	GesturesRecognized uint64  `json:"gesturesRecognized"`
	RecognitionRate    float64 `json:"recognitionRate"`
	AvgLatencyMs       float64 `json:"avgLatencyMs"`
	MaxLatencyMs       uint32  `json:"maxLatencyMs"`
	MinBatteryLevel    float64 `json:"minBatteryLevel"`
	AvgBatteryLevel    float64 `json:"avgBatteryLevel"`
	AvgSignalQuality   float64 `json:"avgSignalQuality"`
	ErrorEvents        uint64  `json:"errorEvents"`
}

type Report struct {
	Username    string   `json:"username"`
	Client      *Client  `json:"client,omitempty"`
	Prostheses  []string `json:"prostheses"`
	Period      Period   `json:"period"`
	Totals      Totals   `json:"totals"`
	Days        []Day    `json:"days"`
	GeneratedAt string   `json:"generatedAt"`
}

func Build(username string, days []Day, from, to, requestedTo, coveredUntil time.Time) *Report {
	report := &Report{
		Username:   username,
		Prostheses: []string{},
		Period: Period{
			From:             from.Format(dateLayout),
			To:               to.Format(dateLayout),
			RequestedTo:      requestedTo.Format(dateLayout),
			DataCoveredUntil: coveredUntil.Format(dateLayout),
			Truncated:        requestedTo.After(coveredUntil),
		},
		Days:        days,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if report.Days == nil {
		report.Days = []Day{}
	}

	seen := map[string]struct{}{}
	dates := map[string]struct{}{}

	var latencyWeighted, batteryWeighted, signalWeighted float64
	minBattery := -1.0

	for _, day := range days {
		if _, ok := seen[day.ProsthesisSerial]; !ok {
			seen[day.ProsthesisSerial] = struct{}{}
			report.Prostheses = append(report.Prostheses, day.ProsthesisSerial)
		}
		dates[day.Date.Format(dateLayout)] = struct{}{}

		report.Totals.ReadingsTotal += day.ReadingsTotal
		report.Totals.GesturesRecognized += day.GesturesRecognized
		report.Totals.ErrorEvents += day.ErrorEvents

		if day.MaxLatencyMs > report.Totals.MaxLatencyMs {
			report.Totals.MaxLatencyMs = day.MaxLatencyMs
		}
		if minBattery < 0 || day.MinBatteryLevel < minBattery {
			minBattery = day.MinBatteryLevel
		}

		weight := float64(day.ReadingsTotal)
		latencyWeighted += day.AvgLatencyMs * weight
		batteryWeighted += day.AvgBatteryLevel * weight
		signalWeighted += day.AvgSignalQuality * weight
	}

	report.Totals.Days = len(dates)
	if minBattery >= 0 {
		report.Totals.MinBatteryLevel = round2(minBattery)
	}
	if total := float64(report.Totals.ReadingsTotal); total > 0 {
		report.Totals.RecognitionRate = round4(float64(report.Totals.GesturesRecognized) / total)
		report.Totals.AvgLatencyMs = round2(latencyWeighted / total)
		report.Totals.AvgBatteryLevel = round2(batteryWeighted / total)
		report.Totals.AvgSignalQuality = round2(signalWeighted / total)
	}

	return report
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func round4(v float64) float64 { return float64(int64(v*10000+0.5)) / 10000 }
