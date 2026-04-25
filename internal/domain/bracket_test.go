package domain

import "testing"

func TestGenerateBracket(t *testing.T) {
	t.Parallel()

	for _, count := range []int{2, 3, 4, 5, 6, 7, 8} {
		players := make([]string, count)
		for i := range players {
			players[i] = string(rune('A' + i))
		}
		matches := GenerateBracket(players)
		if len(matches) == 0 {
			t.Fatalf("expected matches for %d players", count)
		}
	}
}
