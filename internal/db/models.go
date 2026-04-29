package db

type User struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	AvatarURL    *string `json:"avatar_url"`
	IsSuperAdmin bool    `json:"is_super_admin"`
	CreatedAt    int64   `json:"created_at"`
}

type Session struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	ExpiresAt int64   `json:"expires_at"`
	CreatedAt int64   `json:"created_at"`
	UserAgent *string `json:"user_agent"`
	IP        *string `json:"ip"`
}

type Space struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	IsInviteOnly bool    `json:"is_invite_only"`
	JoinCode     *string `json:"join_code"`
	CreatedBy    string  `json:"created_by"`
	CreatedAt    int64   `json:"created_at"`
	DeletedAt    *int64  `json:"deleted_at"`
}

type SpaceInvitation struct {
	ID         string `json:"id"`
	SpaceID    string `json:"space_id"`
	Email      string `json:"email"`
	Token      string `json:"token"`
	InvitedBy  string `json:"invited_by"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"created_at"`
	AcceptedAt *int64 `json:"accepted_at"`
}

type SpaceJoinRequest struct {
	ID         string  `json:"id"`
	SpaceID    string  `json:"space_id"`
	UserID     string  `json:"user_id"`
	JoinCode   string  `json:"join_code"`
	Status     string  `json:"status"`
	Message    *string `json:"message"`
	CreatedAt  int64   `json:"created_at"`
	ReviewedAt *int64  `json:"reviewed_at"`
	ReviewedBy *string `json:"reviewed_by"`
}

type Player struct {
	ID          string  `json:"id"`
	SpaceID     string  `json:"space_id"`
	DisplayName string  `json:"display_name"`
	UserID      *string `json:"user_id"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   int64   `json:"created_at"`
	DeletedAt   *int64  `json:"deleted_at"`
}

type Match struct {
	ID                     string  `json:"id"`
	SpaceID                string  `json:"space_id"`
	Kind                   string  `json:"kind"`
	TournamentID           *string `json:"tournament_id"`
	TournamentPhase        *string `json:"tournament_phase"`
	TournamentGroupID      *string `json:"tournament_group_id"`
	TournamentBracketRound *int    `json:"tournament_bracket_round"`
	BestOf                 int     `json:"best_of"`
	PointsToWin            int     `json:"points_to_win"`
	Status                 string  `json:"status"`
	WinnerSide             *string `json:"winner_side"`
	StartedAt              int64   `json:"started_at"`
	CompletedAt            *int64  `json:"completed_at"`
	CreatedBy              string  `json:"created_by"`
	CreatedAt              int64   `json:"created_at"`
	UpdatedAt              int64   `json:"updated_at"`
	DeletedAt              *int64  `json:"deleted_at"`
	LegacyCosmosID         *string `json:"legacy_cosmos_id"`
}

type Game struct {
	ID           string `json:"id"`
	MatchID      string `json:"match_id"`
	GameNumber   int    `json:"game_number"`
	HomeScore    int    `json:"home_score"`
	VisitorScore int    `json:"visitor_score"`
	Status       string `json:"status"`
	StartedAt    int64  `json:"started_at"`
	CompletedAt  *int64 `json:"completed_at"`
}

type ScoreEvent struct {
	ID                string `json:"id"`
	GameID            string `json:"game_id"`
	Sequence          int    `json:"sequence"`
	ScorerSide        string `json:"scorer_side"`
	HomeScoreAfter    int    `json:"home_score_after"`
	VisitorScoreAfter int    `json:"visitor_score_after"`
	OccurredAt        int64  `json:"occurred_at"`
}

type Tournament struct {
	ID             string  `json:"id"`
	SpaceID        string  `json:"space_id"`
	Name           string  `json:"name"`
	Format         string  `json:"format"`
	BestOf         int     `json:"best_of"`
	PointsToWin    int     `json:"points_to_win"`
	Status         string  `json:"status"`
	WinnerPlayerID *string `json:"winner_player_id"`
	CreatedBy      string  `json:"created_by"`
	CreatedAt      int64   `json:"created_at"`
	UpdatedAt      int64   `json:"updated_at"`
	DeletedAt      *int64  `json:"deleted_at"`
	LegacyCosmosID *string `json:"legacy_cosmos_id"`
}
