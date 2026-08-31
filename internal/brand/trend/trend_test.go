package trend

import (
	"testing"
	"time"
)

func TestTracker_AddPoint(t *testing.T) {
	tracker := NewTracker("test-brand", 10)

	// Add some points
	for i := 0; i < 5; i++ {
		tracker.AddPoint(TrendPoint{
			Timestamp:    time.Now().Add(time.Duration(i) * 24 * time.Hour),
			BVSScore:     float64(50 + i*5),
			MentionRate:  float64(30+i*10) / 100,
			CitationRate: float64(10+i*5) / 100,
		})
	}

	series := tracker.GetSeries()
	if len(series.Points) != 5 {
		t.Errorf("expected 5 points, got %d", len(series.Points))
	}
	if series.Summary.TotalChecks != 5 {
		t.Errorf("expected 5 total checks, got %d", series.Summary.TotalChecks)
	}
}

func TestTracker_MaxPoints(t *testing.T) {
	tracker := NewTracker("test-brand", 3)

	for i := 0; i < 5; i++ {
		tracker.AddPoint(TrendPoint{
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			BVSScore:  float64(i * 10),
		})
	}

	series := tracker.GetSeries()
	if len(series.Points) != 3 {
		t.Errorf("expected 3 points (max), got %d", len(series.Points))
	}
	// Should keep the last 3
	if series.Points[0].BVSScore != 20 {
		t.Errorf("expected first point to be 20, got %f", series.Points[0].BVSScore)
	}
}

func TestTracker_DetectAlerts(t *testing.T) {
	tracker := NewTracker("test-brand", 100)

	// Add points with a sudden drop
	for i := 0; i < 5; i++ {
		score := 60.0
		if i == 4 {
			score = 30.0 // sudden drop
		}
		tracker.AddPoint(TrendPoint{
			Timestamp: time.Now().Add(time.Duration(i) * 24 * time.Hour),
			BVSScore:  score,
		})
	}

	series := tracker.GetSeries()
	if len(series.Summary.Alerts) == 0 {
		t.Error("expected alerts for sudden drop")
	}
}

func TestTracker_ChartData(t *testing.T) {
	tracker := NewTracker("test-brand", 10)

	tracker.AddPoint(TrendPoint{
		Timestamp:    time.Now(),
		BVSScore:     75,
		MentionRate:  0.6,
		CitationRate: 0.3,
	})

	chartData := tracker.ChartData()
	if chartData == nil {
		t.Error("expected chart data")
	}
}

func TestFormatPercent(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{15.0, "+15.00%"},
		{-10.5, "-10.50%"},
		{0, "0.00%"},
	}
	for _, tt := range tests {
		result := formatPercent(tt.input)
		if result != tt.expected {
			t.Errorf("formatPercent(%f) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}
