package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCalculateRound verifies the core 6-player Pinochle variant scoring algorithms.
func TestCalculateRound(t *testing.T) {
	teams := []string{"Team A", "Team B", "Team C"}

	// Case 1: Bidding team saves bid, non-bidding teams save melds (tricks >= 10)
	t.Run("AllSaved", func(t *testing.T) {
		melds := map[string]int{"Team A": 50, "Team B": 30, "Team C": 20}
		tricks := map[string]int{"Team A": 40, "Team B": 20, "Team C": 15}
		bidAmount := 80

		round, err := CalculateRound("Team A", bidAmount, melds, tricks, teams)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Team A (Bidding Team): Meld 50 + Tricks 40 = 90 >= 80. Score should be 90. SavedMeld should be true.
		statA := round.TeamStats["Team A"]
		if statA.RoundScore != 90 || !statA.SavedMeld {
			t.Errorf("Team A expected score 90, saved=true; got score %d, saved=%v", statA.RoundScore, statA.SavedMeld)
		}

		// Team B (Non-Bidding Team): Tricks 20 >= 10. Score should be Meld 30 + Tricks 20 = 50. SavedMeld should be true.
		statB := round.TeamStats["Team B"]
		if statB.RoundScore != 50 || !statB.SavedMeld {
			t.Errorf("Team B expected score 50, saved=true; got score %d, saved=%v", statB.RoundScore, statB.SavedMeld)
		}

		// Team C (Non-Bidding Team): Tricks 15 >= 10. Score should be Meld 20 + Tricks 15 = 35. SavedMeld should be true.
		statC := round.TeamStats["Team C"]
		if statC.RoundScore != 35 || !statC.SavedMeld {
			t.Errorf("Team C expected score 35, saved=true; got score %d, saved=%v", statC.RoundScore, statC.SavedMeld)
		}
	})

	// Case 2: Bidding team goes set (went set), non-bidding team fails to save meld (tricks < 10)
	t.Run("BiddingSetAndNonBiddingFailedSave", func(t *testing.T) {
		melds := map[string]int{"Team A": 30, "Team B": 40, "Team C": 10}
		tricks := map[string]int{"Team A": 35, "Team B": 35, "Team C": 5}
		bidAmount := 80 // Team A bid 80. Meld 30 + Tricks 35 = 65 < 80. Must go set.

		round, err := CalculateRound("Team A", bidAmount, melds, tricks, teams)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Team A (Bidding Team): 65 < 80. Score = -80. SavedMeld = false.
		statA := round.TeamStats["Team A"]
		if statA.RoundScore != -80 || statA.SavedMeld {
			t.Errorf("Team A expected score -80, saved=false; got score %d, saved=%v", statA.RoundScore, statA.SavedMeld)
		}

		// Team B (Non-Bidding Team): Tricks 35 >= 10. Score = Meld 40 + Tricks 35 = 75. SavedMeld = true.
		statB := round.TeamStats["Team B"]
		if statB.RoundScore != 75 || !statB.SavedMeld {
			t.Errorf("Team B expected score 75, saved=true; got score %d, saved=%v", statB.RoundScore, statB.SavedMeld)
		}

		// Team C (Non-Bidding Team): Tricks 5 < 10. Meld 10 is wiped. Score = Tricks 5 = 5. SavedMeld = false.
		statC := round.TeamStats["Team C"]
		if statC.RoundScore != 5 || statC.SavedMeld {
			t.Errorf("Team C expected score 5, saved=false; got score %d, saved=%v", statC.RoundScore, statC.SavedMeld)
		}
	})

	// Case 3: Invalid input parameters
	t.Run("InputValidations", func(t *testing.T) {
		melds := map[string]int{"Team A": 20, "Team B": 20, "Team C": 20}
		
		// Trick sum is 80 (not 75)
		tricksInvalidSum := map[string]int{"Team A": 40, "Team B": 20, "Team C": 20}
		_, err := CalculateRound("Team A", 80, melds, tricksInvalidSum, teams)
		if err == nil {
			t.Error("Expected error for tricks sum != 75, got nil")
		}

		// Bid amount too low (< 60)
		tricksValidSum := map[string]int{"Team A": 40, "Team B": 20, "Team C": 15}
		_, err = CalculateRound("Team A", 55, melds, tricksValidSum, teams)
		if err == nil {
			t.Error("Expected error for bid amount < 60, got nil")
		}

		// Invalid bidding team
		_, err = CalculateRound("NonExistentTeam", 80, melds, tricksValidSum, teams)
		if err == nil {
			t.Error("Expected error for non-existent bidding team, got nil")
		}
	})

	// Case 4: Meld < 20 gets treated as 0
	t.Run("MeldBelow20TreatedAsZero", func(t *testing.T) {
		melds := map[string]int{"Team A": 50, "Team B": 15, "Team C": 20}
		tricks := map[string]int{"Team A": 40, "Team B": 20, "Team C": 15}
		bidAmount := 80

		round, err := CalculateRound("Team A", bidAmount, melds, tricks, teams)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Team B has meld 15 < 20. It should be treated as 0 meld.
		// Since tricks 20 >= 10, meld is saved (but is 0). Score = Meld 0 + Tricks 20 = 20.
		statB := round.TeamStats["Team B"]
		if statB.RoundScore != 20 {
			t.Errorf("Team B with meld 15 expected score 20, got score %d", statB.RoundScore)
		}
	})

	// Case 5: Aborted round (e.g. no trump marriage) with 0 tricks and 0 melds
	t.Run("AbortedRoundNoMarriage", func(t *testing.T) {
		melds := map[string]int{"Team A": 0, "Team B": 0, "Team C": 0}
		tricks := map[string]int{"Team A": 0, "Team B": 0, "Team C": 0}
		bidAmount := 60

		round, err := CalculateRound("Team A", bidAmount, melds, tricks, teams)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Team A should go set: Score = -60, SavedMeld = false
		statA := round.TeamStats["Team A"]
		if statA.RoundScore != -60 || statA.SavedMeld {
			t.Errorf("Team A expected score -60, saved=false; got score %d, saved=%v", statA.RoundScore, statA.SavedMeld)
		}

		// Team B and C should score 0
		statB := round.TeamStats["Team B"]
		if statB.RoundScore != 0 || statB.SavedMeld {
			t.Errorf("Team B expected score 0, saved=false; got score %d, saved=%v", statB.RoundScore, statB.SavedMeld)
		}
	})
}

// TestGameManagerLifecycle validates starting a match, logging rounds, persistent YAML updates, and archiving matches.
func TestGameManagerLifecycle(t *testing.T) {
	// Create temporary file path for database test
	tmpDir, err := os.MkdirTemp("", "pinochle-test")
	if err != nil {
		t.Fatalf("Failed to create temporary dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_history.yaml")

	// 1. Initialize GameManager
	mgr, err := NewGameManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create GameManager: %v", err)
	}

	state := mgr.GetAppState()
	if len(state.History) != 0 || state.ActiveGame != nil {
		t.Errorf("Expected clean empty initial state, got history=%d active=%v", len(state.History), state.ActiveGame)
	}

	// 2. Start a new game
	teams := []string{"Alpha", "Beta", "Gamma"}
	players := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank"}
	game, err := mgr.StartGame(teams, players)
	if err != nil {
		t.Fatalf("Failed to start game: %v", err)
	}

	if game.ID != 1 || game.Status != "active" || len(game.Teams) != 3 || len(game.Players) != 6 {
		t.Errorf("Unexpected active game configuration: %+v", game)
	}

	// 3. Submit a round
	roundPayload := SubmitRoundPayload{
		BiddingPlayer: "Alice",
		BiddingTeam:   "Alpha",
		BidAmount:     75,
		TrumpSuit:     "Spades",
		Melds:         map[string]int{"Alpha": 30, "Beta": 20, "Gamma": 10},
		Tricks:        map[string]int{"Alpha": 45, "Beta": 25, "Gamma": 5}, // Sum = 75
	}

	updatedGame, err := mgr.SubmitRound(roundPayload)
	if err != nil {
		t.Fatalf("Failed to submit round: %v", err)
	}

	if len(updatedGame.Rounds) != 1 {
		t.Errorf("Expected 1 round, got %d", len(updatedGame.Rounds))
	}
	if updatedGame.Rounds[0].BiddingPlayer != "Alice" {
		t.Errorf("Expected bidding player to be Alice, got %q", updatedGame.Rounds[0].BiddingPlayer)
	}
	if updatedGame.Rounds[0].TrumpSuit != "Spades" {
		t.Errorf("Expected trump suit to be Spades, got %q", updatedGame.Rounds[0].TrumpSuit)
	}

	// Score checks:
	// Alpha: Meld 30 + Tricks 45 = 75 >= 75. Score = 75.
	// Beta: Tricks 25 >= 10. Score = Meld 20 + Tricks 25 = 45.
	// Gamma: Tricks 5 < 10. Meld 10 wiped. Score = Tricks 5 = 5.
	if updatedGame.CurrentTotals["Alpha"] != 75 {
		t.Errorf("Alpha score expected 75, got %d", updatedGame.CurrentTotals["Alpha"])
	}
	if updatedGame.CurrentTotals["Beta"] != 45 {
		t.Errorf("Beta score expected 45, got %d", updatedGame.CurrentTotals["Beta"])
	}
	if updatedGame.CurrentTotals["Gamma"] != 5 {
		t.Errorf("Gamma score expected 5, got %d", updatedGame.CurrentTotals["Gamma"])
	}

	// 4. Finalize the game
	finalState, err := mgr.FinalizeGame(game.SessionID)
	if err != nil {
		t.Fatalf("Failed to finalize game: %v", err)
	}

	if finalState.ActiveGame != nil {
		t.Error("Expected activeGame to be nil after finalization")
	}
	if len(finalState.History) != 1 || finalState.History[0].Status != "completed" {
		t.Errorf("Expected 1 completed game in history, got: %+v", finalState.History)
	}

	// 5. Reload database state and check persistence
	newMgr, err := NewGameManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to rebuild GameManager: %v", err)
	}

	reloadedState := newMgr.GetAppState()
	if len(reloadedState.History) != 1 || reloadedState.ActiveGame != nil {
		t.Errorf("Persistence check failed. Loaded history count: %d, active: %v", len(reloadedState.History), reloadedState.ActiveGame)
	}

	persistedGame := reloadedState.History[0]
	if persistedGame.CurrentTotals["Alpha"] != 75 || persistedGame.CurrentTotals["Beta"] != 45 || persistedGame.CurrentTotals["Gamma"] != 5 {
		t.Errorf("Persisted score totals mismatch: %+v", persistedGame.CurrentTotals)
	}
	if len(persistedGame.Rounds) != 1 || persistedGame.Rounds[0].TrumpSuit != "Spades" {
		t.Errorf("Persisted round trump suit expected 'Spades', got %q", persistedGame.Rounds[0].TrumpSuit)
	}
}

// TestGameManagerSessionID verifies the shareable Session ID features.
func TestGameManagerSessionID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pinochle-session-test")
	if err != nil {
		t.Fatalf("Failed to create temporary dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_session_history.yaml")
	mgr, err := NewGameManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create GameManager: %v", err)
	}

	// Start a game and verify session ID generation
	teams := []string{"T1", "T2", "T3"}
	players := []string{"P1", "P2", "P3", "P4", "P5", "P6"}
	game, err := mgr.StartGame(teams, players)
	if err != nil {
		t.Fatalf("Failed to start game: %v", err)
	}

	if len(game.SessionID) != 6 {
		t.Errorf("Expected 6-character Session ID, got %q", game.SessionID)
	}

	// Verify AppState lookup by Session ID
	state := mgr.GetAppStateForSession(game.SessionID)
	if state.ActiveGame == nil || state.ActiveGame.SessionID != game.SessionID {
		t.Errorf("Expected to retrieve active game by session ID %q", game.SessionID)
	}

	// Verify AppState lookup by incorrect Session ID returns default/main active game
	stateWrong := mgr.GetAppStateForSession("INVALID")
	if stateWrong.ActiveGame == nil || stateWrong.ActiveGame.SessionID != game.SessionID {
		t.Errorf("Expected fallback to main active game for invalid session ID")
	}

	// Submit a round with the session ID
	roundPayload := SubmitRoundPayload{
		SessionID:     game.SessionID,
		BiddingPlayer: "P1",
		BiddingTeam:   "T1",
		BidAmount:     75,
		Melds:         map[string]int{"T1": 30, "T2": 20, "T3": 10},
		Tricks:        map[string]int{"T1": 45, "T2": 25, "T3": 5},
	}

	updatedGame, err := mgr.SubmitRound(roundPayload)
	if err != nil {
		t.Fatalf("Failed to submit round: %v", err)
	}
	if len(updatedGame.Rounds) != 1 {
		t.Errorf("Expected 1 round, got %d", len(updatedGame.Rounds))
	}

	// Cancel the game using the session ID
	cancelState, err := mgr.CancelGame(game.SessionID)
	if err != nil {
		t.Fatalf("Failed to cancel game: %v", err)
	}
	if cancelState.ActiveGame != nil {
		t.Error("Expected active game to be nil after cancelation")
	}
}

// TestGameManager4Players verifies starting and playing a 4-player game.
func TestGameManager4Players(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pinochle-4p-test")
	if err != nil {
		t.Fatalf("Failed to create temporary dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_4p_history.yaml")
	mgr, err := NewGameManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create GameManager: %v", err)
	}

	// Start a 4-player game (2 teams, 4 players)
	teams := []string{"Alpha", "Beta"}
	players := []string{"Alice", "Bob", "Charlie", "David"}
	game, err := mgr.StartGame(teams, players)
	if err != nil {
		t.Fatalf("Failed to start 4-player game: %v", err)
	}

	if game.SessionID == "" {
		t.Error("Expected game to have a session ID")
	}

	// Submit a round verifying 50 trick points sum validation
	roundPayload := SubmitRoundPayload{
		SessionID:     game.SessionID,
		BiddingPlayer: "Alice",
		BiddingTeam:   "Alpha",
		BidAmount:     75,
		Melds:         map[string]int{"Alpha": 45, "Beta": 20},
		Tricks:        map[string]int{"Alpha": 30, "Beta": 20}, // Sum = 50
	}

	updatedGame, err := mgr.SubmitRound(roundPayload)
	if err != nil {
		t.Fatalf("Failed to submit round: %v", err)
	}

	if updatedGame.CurrentTotals["Alpha"] != 75 { // Meld 45 + Tricks 30 = 75
		t.Errorf("Alpha score expected 75, got %d", updatedGame.CurrentTotals["Alpha"])
	}
	if updatedGame.CurrentTotals["Beta"] != 40 { // Meld 20 + Tricks 20 = 40 (Tricks 20 >= 10, saves meld)
		t.Errorf("Beta score expected 40, got %d", updatedGame.CurrentTotals["Beta"])
	}
}

func TestOnlineMultiplayerGameplay(t *testing.T) {
	// 1. Test Deck Theme conversions and sorting
	// Classic
	handClassic := []Card{
		{Suit: "Spades", Rank: "Q"},
		{Suit: "Diamonds", Rank: "J"},
		{Suit: "Hearts", Rank: "A"},
	}
	SortCards(handClassic, false)
	if handClassic[0].Suit != "Spades" {
		t.Errorf("Expected first suit to be Spades after sorting, got %s", handClassic[0].Suit)
	}

	// Number
	handNumber := []Card{
		{Suit: "Blue", Rank: "5"}, // Jacks are 5
		{Suit: "Red", Rank: "4"},  // Queens are 4
		{Suit: "Green", Rank: "1"}, // Aces are 1
	}
	SortCards(handNumber, true)
	if handNumber[0].Suit != "Red" {
		t.Errorf("Expected first suit to be Red after sorting, got %s", handNumber[0].Suit)
	}

	// 2. Test Meld Evaluation under Classic vs Number themes
	// Pinochle: Spades Q and Diamonds J = 4 points
	pinochleHandClassic := []Card{
		{Suit: "Spades", Rank: "Q"},
		{Suit: "Diamonds", Rank: "J"},
	}
	meldClassic := EvaluateMeld(pinochleHandClassic, "Hearts", false)
	if meldClassic != 4 {
		t.Errorf("Expected pinochle meld to be 4, got %d", meldClassic)
	}

	// Pinochle under Number deck: Red 4 and Blue 5 = 4 points
	pinochleHandNumber := []Card{
		{Suit: "Red", Rank: "4"},
		{Suit: "Blue", Rank: "5"},
	}
	meldNumber := EvaluateMeld(pinochleHandNumber, "Yellow", true)
	if meldNumber != 4 {
		t.Errorf("Expected number-pinochle meld to be 4, got %d", meldNumber)
	}

	// 3. Test Trick play validity constraints
	// Lead Spade A. Hand has Spades K and Hearts 10 (trump is Hearts). Player must follow suit (Spade).
	trick := []TrickCard{
		{Player: "P1", Card: Card{Suit: "Spades", Rank: "A"}},
	}
	hand := []Card{
		{Suit: "Spades", Rank: "K"},
		{Suit: "Hearts", Rank: "10"},
	}
	valid := GetValidCards(hand, trick, "Hearts", false)
	if len(valid) != 1 || valid[0].Suit != "Spades" {
		t.Errorf("Expected player to be forced to follow suit (Spades K), got: %v", valid)
	}

	// 4. Test Trick Winner resolution
	game := &OnlineGame{
		SessionID:  "testcode",
		MaxPlayers: 4,
		TrumpSuit:  "Hearts",
		DeckTheme:  "classic",
		Players: []Player{
			{Name: "P1", IsBot: false, Hand: []Card{}},
			{Name: "P2", IsBot: false, Hand: []Card{}},
			{Name: "P3", IsBot: false, Hand: []Card{}},
			{Name: "P4", IsBot: false, Hand: []Card{}},
		},
		TricksWon:      map[string]int{"Team 1": 0, "Team 2": 0},
		TeamMeldScores: map[string]int{"Team 1": 0, "Team 2": 0},
		Scores:         map[string]int{"Team 1": 0, "Team 2": 0},
	}
	// Play trick cards:
	// P1 plays Spade K
	// P2 plays Spade 10
	// P3 plays Spade A (winning suit)
	// P4 plays Heart J (trump, wins overall)
	playCardInGame(game, 0, Card{Suit: "Spades", Rank: "K"})
	playCardInGame(game, 1, Card{Suit: "Spades", Rank: "10"})
	playCardInGame(game, 2, Card{Suit: "Spades", Rank: "A"})
	playCardInGame(game, 3, Card{Suit: "Hearts", Rank: "J"})

	// P4 wins trick because of trump Heart J. Team for P4 (seat index 3) is Team 2.
	if game.TrickLeader != 3 {
		t.Errorf("Expected Trick leader to be P4 (seat 3), got %d", game.TrickLeader)
	}

	// 5. Test Cautious Bot Bidding AI
	botGame := &OnlineGame{
		SessionID:  "botcode",
		MaxPlayers: 4,
		TrumpSuit:  "Spades",
		DeckTheme:  "classic",
		Players: []Player{
			{
				Name:  "BotFox",
				IsBot: true,
				Hand: []Card{
					{Suit: "Spades", Rank: "A"}, {Suit: "Spades", Rank: "10"}, {Suit: "Spades", Rank: "K"}, {Suit: "Spades", Rank: "Q"}, {Suit: "Spades", Rank: "J"},
					{Suit: "Spades", Rank: "A"}, {Suit: "Spades", Rank: "10"}, {Suit: "Spades", Rank: "K"}, {Suit: "Spades", Rank: "Q"}, {Suit: "Spades", Rank: "J"},
				},
			},
		},
		CurrentBid: 0,
	}

	bid, pass := getBotBestBid(botGame, 0)
	if pass {
		t.Error("Expected bot to place a bid, not pass")
	}
	if bid < 50 {
		t.Errorf("Expected bid to be at least min bid of 50, got %d", bid)
	}
}

func Test6PlayerBiddingRules(t *testing.T) {
	// Setup 6-player game
	game := &OnlineGame{
		SessionID:  "sixplayer",
		MaxPlayers: 6,
		Status:     "bidding",
		Players: []Player{
			{Name: "P1", Hand: []Card{
				{Suit: "Spades", Rank: "A"}, {Suit: "Spades", Rank: "10"}, {Suit: "Spades", Rank: "K"}, {Suit: "Spades", Rank: "Q"}, {Suit: "Spades", Rank: "J"},
				{Suit: "Spades", Rank: "A"}, {Suit: "Spades", Rank: "10"}, {Suit: "Spades", Rank: "K"}, {Suit: "Spades", Rank: "Q"}, {Suit: "Spades", Rank: "J"},
			}},
			{Name: "P2"},
			{Name: "P3"},
			{Name: "P4"},
			{Name: "P5"},
			{Name: "P6"},
		},
		CurrentBid:   0,
		ActiveBidder: 0,
	}

	// 1. Verify first bid at 60 is allowed
	game.CurrentBid = 0
	// Validate bot selection for first bid: nextBid should be minStartBid which is 60
	bid, pass := getBotBestBid(game, 0)
	if pass {
		t.Error("Expected bot to place a bid on empty board")
	}
	if bid != 60 {
		t.Errorf("Expected bot first bid to be 60, got %d", bid)
	}

	// 2. Verify bot nextBid calculations when currentBid is close to 70
	game.CurrentBid = 68
	bid, pass = getBotBestBid(game, 0)
	// nextBid would normally be 68 + 5 = 73, but must be rounded to a multiple of 5 above 70, which is 75
	if bid != 75 {
		t.Errorf("Expected bot next bid after 68 to be 75, got %d", bid)
	}

	// 3. Verify bot nextBid calculations when currentBid is 70
	game.CurrentBid = 70
	bid, pass = getBotBestBid(game, 0)
	// nextBid is 70 + 5 = 75
	if bid != 75 {
		t.Errorf("Expected bot next bid after 70 to be 75, got %d", bid)
	}
}

