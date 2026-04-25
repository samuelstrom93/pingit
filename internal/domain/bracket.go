package domain

import "math"

type BracketMatch struct {
	Round     int     `json:"round"`
	Position  int     `json:"position"`
	Home      *string `json:"home,omitempty"`
	Visitor   *string `json:"visitor,omitempty"`
	HasBye    bool    `json:"has_bye"`
	NextRound int     `json:"next_round"`
	NextSlot  int     `json:"next_slot"`
}

func GenerateBracket(players []string) []BracketMatch {
	if len(players) == 0 {
		return nil
	}

	size := nextPowerOfTwo(len(players))
	padded := append([]string(nil), players...)
	for len(padded) < size {
		padded = append(padded, "")
	}

	var matches []BracketMatch
	rounds := int(math.Log2(float64(size)))
	for round := 1; round <= rounds; round++ {
		positions := size >> round
		for pos := 0; pos < positions; pos++ {
			match := BracketMatch{
				Round:     round,
				Position:  pos,
				NextRound: round + 1,
				NextSlot:  pos / 2,
			}
			if round == 1 {
				home := padded[pos*2]
				visitor := padded[pos*2+1]
				if home != "" {
					match.Home = &home
				}
				if visitor != "" {
					match.Visitor = &visitor
				}
				match.HasBye = home == "" || visitor == ""
			}
			matches = append(matches, match)
		}
	}
	return matches
}

func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}
