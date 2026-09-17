package model_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

func TestHistoryDateRangeIgnoresServerTimezone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset int
		hour   time.Duration
	}{
		{"west", -8 * 60 * 60, 0},
		{"east", 9 * 60 * 60, 23 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalLocal := time.Local
			time.Local = time.FixedZone(tc.name, tc.offset)
			t.Cleanup(func() { time.Local = originalLocal })
			synctest.Test(t, func(t *testing.T) {
				// The fake clock starts at midnight UTC. Keep the local date
				// outside the UTC day regardless of the real wall clock.
				time.Sleep(tc.hour + 30*time.Minute)
				testutil.InitModel(t)
				if err := model.UpdateHistory(0, "q", "https://example.com", "Example"); err != nil {
					t.Fatalf("UpdateHistory() error: %v", err)
				}

				now := time.Now().UTC()
				dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
				dayEnd := dayStart.AddDate(0, 0, 1)
				timestamps, err := model.GetHistoryItemTimestampsFilteredByDate(0, "", dayStart.Unix(), dayEnd.Unix())
				if err != nil {
					t.Fatalf("GetHistoryItemTimestampsFilteredByDate() error: %v", err)
				}
				if len(timestamps) != 1 {
					t.Fatalf("timestamps in the current UTC day = %d, want 1", len(timestamps))
				}
				if got := timestamps[0]; !got.Equal(now) {
					t.Errorf("stored timestamp = %s, want %s", got, now)
				}
			})
		})
	}
}
