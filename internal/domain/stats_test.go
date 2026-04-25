package domain

import "testing"

func TestStatsHelpers(t *testing.T) {
	t.Parallel()

	if got := WinRate(3, 4); got != 0.75 {
		t.Fatalf("WinRate() = %v, want 0.75", got)
	}

	form := RecentForm([]MatchOutcome{{Won: true}, {Won: false}, {Won: true}}, 10)
	if len(form) != 3 || form[0] != "W" || form[1] != "L" || form[2] != "W" {
		t.Fatalf("RecentForm() = %#v", form)
	}

	if got := CurrentStreak([]MatchOutcome{{Won: true}, {Won: true}, {Won: false}, {Won: false}}); got != "L2" {
		t.Fatalf("CurrentStreak() = %q, want L2", got)
	}
}
