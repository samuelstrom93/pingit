package domain

type GameState struct {
	Home    int `json:"home"`
	Visitor int `json:"visitor"`
}

type ScoreResult struct {
	GameState      GameState `json:"game_state"`
	GameCompleted  bool      `json:"game_completed"`
	MatchCompleted bool      `json:"match_completed"`
	WinnerSide     string    `json:"winner_side,omitempty"`
}

func ApplyScore(pointsToWin int, home, visitor int, side string) ScoreResult {
	if side == "home" {
		home++
	} else {
		visitor++
	}

	result := ScoreResult{GameState: GameState{Home: home, Visitor: visitor}}
	if IsGameComplete(pointsToWin, home, visitor) {
		result.GameCompleted = true
		if home > visitor {
			result.WinnerSide = "home"
		} else {
			result.WinnerSide = "visitor"
		}
	}
	return result
}

func IsGameComplete(pointsToWin, home, visitor int) bool {
	if home < pointsToWin && visitor < pointsToWin {
		return false
	}
	diff := home - visitor
	if diff < 0 {
		diff = -diff
	}
	return diff >= 2
}

func MatchWinner(bestOf int, completedGames []GameState) (string, bool) {
	needed := bestOf/2 + 1
	homeWins := 0
	visitorWins := 0
	for _, game := range completedGames {
		if game.Home > game.Visitor {
			homeWins++
		}
		if game.Visitor > game.Home {
			visitorWins++
		}
	}
	if homeWins >= needed {
		return "home", true
	}
	if visitorWins >= needed {
		return "visitor", true
	}
	return "", false
}
