package domain

import "testing"

func TestIsGameComplete(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		pointsToWin int
		home        int
		visitor     int
		want        bool
	}{
		{"eleven-nine", 11, 11, 9, true},
		{"eleven-seven", 11, 11, 7, true},
		{"thirteen-eleven", 11, 13, 11, true},
		{"twelve-ten", 11, 12, 10, true},
		{"deuce-ongoing", 11, 11, 10, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsGameComplete(tc.pointsToWin, tc.home, tc.visitor); got != tc.want {
				t.Fatalf("IsGameComplete() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchWinner(t *testing.T) {
	t.Parallel()

	if winner, ok := MatchWinner(3, []GameState{{11, 7}, {11, 9}}); !ok || winner != "home" {
		t.Fatalf("best-of-3 two-nil winner = %q %v", winner, ok)
	}
	if winner, ok := MatchWinner(3, []GameState{{9, 11}, {11, 9}, {11, 8}}); !ok || winner != "home" {
		t.Fatalf("best-of-3 two-one winner = %q %v", winner, ok)
	}
	if winner, ok := MatchWinner(5, []GameState{{11, 7}, {9, 11}, {11, 9}, {11, 8}}); !ok || winner != "home" {
		t.Fatalf("best-of-5 winner = %q %v", winner, ok)
	}
}
