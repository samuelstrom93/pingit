package domain

type Pairing struct {
	Home string `json:"home"`
	Away string `json:"away"`
}

func RoundRobinPairs(players []string) []Pairing {
	if len(players) < 2 {
		return nil
	}

	work := append([]string(nil), players...)
	if len(work)%2 == 1 {
		work = append(work, "")
	}

	n := len(work)
	rounds := n - 1
	pairs := make([]Pairing, 0, len(players)*(len(players)-1)/2)

	for round := 0; round < rounds; round++ {
		for i := 0; i < n/2; i++ {
			home := work[i]
			away := work[n-1-i]
			if home != "" && away != "" {
				pairs = append(pairs, Pairing{Home: home, Away: away})
			}
		}
		work = rotate(work)
	}
	return pairs
}

func rotate(players []string) []string {
	if len(players) <= 2 {
		return players
	}
	next := make([]string, len(players))
	next[0] = players[0]
	next[1] = players[len(players)-1]
	copy(next[2:], players[1:len(players)-1])
	return next
}
