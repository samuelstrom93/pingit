package domain

import "testing"

func TestRoundRobinPairs(t *testing.T) {
	t.Parallel()

	for _, count := range []int{3, 4, 5, 6} {
		players := make([]string, count)
		for i := range players {
			players[i] = string(rune('A' + i))
		}
		pairs := RoundRobinPairs(players)
		want := count * (count - 1) / 2
		if len(pairs) != want {
			t.Fatalf("%d players: got %d pairs, want %d", count, len(pairs), want)
		}
	}
}
