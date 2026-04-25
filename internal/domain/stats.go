package domain

import "fmt"

type MatchOutcome struct {
	Won bool
}

func WinRate(wins, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(wins) / float64(total)
}

func RecentForm(outcomes []MatchOutcome, limit int) []string {
	if limit <= 0 || len(outcomes) == 0 {
		return nil
	}
	if len(outcomes) > limit {
		outcomes = outcomes[len(outcomes)-limit:]
	}
	form := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		if outcome.Won {
			form = append(form, "W")
		} else {
			form = append(form, "L")
		}
	}
	return form
}

func CurrentStreak(outcomes []MatchOutcome) string {
	if len(outcomes) == 0 {
		return "0"
	}
	last := outcomes[len(outcomes)-1].Won
	count := 0
	for i := len(outcomes) - 1; i >= 0; i-- {
		if outcomes[i].Won != last {
			break
		}
		count++
	}
	if last {
		return fmt.Sprintf("W%d", count)
	}
	return fmt.Sprintf("L%d", count)
}
