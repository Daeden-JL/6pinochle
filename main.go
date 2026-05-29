package main

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	mrand "math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed templates
var templateFS embed.FS

const HistoryFileName = "games_history.yaml"
const LatestAndroidVersion = "1.0.1"

// --- Online Multiplayer & Card Play Engine Structs ---

type Card struct {
	Suit string `json:"suit"`
	Rank string `json:"rank"`
}

type Player struct {
	Name        string `json:"name"`
	IsBot       bool   `json:"isBot"`
	IsConnected bool   `json:"isConnected"`
	Hand        []Card `json:"hand,omitempty"`
	Ready       bool   `json:"ready"`
	SeatIndex   int    `json:"seatIndex"`
	MeldCards   []Card `json:"meldCards"`
}

type TrickCard struct {
	Player string `json:"player"`
	Card   Card   `json:"card"`
}

type BidInfo struct {
	Player string `json:"player"`
	Bid    int    `json:"bid"`
	Pass   bool   `json:"pass"`
}

type OnlineGame struct {
	SessionID       string               `json:"sessionId"`
	Status          string               `json:"status"` // "lobby", "bidding", "meld", "playing", "summary"
	DeckTheme       string               `json:"deckTheme"` // "classic" or "number"
	DisableReadyUp  bool                 `json:"disableReadyUp"`
	HostName        string               `json:"hostName"`
	MaxPlayers      int                  `json:"maxPlayers"` // 4 or 6
	Players         []Player             `json:"players"`    // Length 4 or 6
	Observers       []string             `json:"observers"`
	BiddingHistory  []BidInfo            `json:"biddingHistory"`
	CurrentBid      int                  `json:"currentBid"`
	HighestBidder   int                  `json:"highestBidder"`
	ActiveBidder    int                  `json:"activeBidder"`
	TrumpSuit       string               `json:"trumpSuit"`
	MeldsDeclared   map[string]int       `json:"meldsDeclared"`
	CurrentTrick    []TrickCard          `json:"currentTrick"`
	TrickLeader     int                  `json:"trickLeader"`
	TricksWon       map[string]int       `json:"tricksWon"`
	TeamMeldScores  map[string]int       `json:"teamMeldScores"`
	Scores          map[string]int       `json:"scores"`
	RoundsCompleted []Round              `json:"roundsCompleted"`
	RoundNumber     int                  `json:"roundNumber"`
	LastActive      time.Time            `json:"lastActive"`
	MeldShowStarted time.Time            `json:"meldShowStarted"`
	TeamNames          []string             `json:"teamNames"`
	LastCompletedTrick []TrickCard          `json:"lastCompletedTrick"`
	LastTrickWinner    string               `json:"lastTrickWinner"`
}

var (
	onlineGames         = make(map[string]*OnlineGame)
	onlineGamesMu       sync.Mutex
	activeBotRoutines   = make(map[string]bool)
	activeBotRoutinesMu sync.Mutex
)

var botNames = []string{
	"JumpingFrog", "SprintingCheetah", "ProwlingTiger", "FlyingEagle",
	"SwimmingDolphin", "ClimbingKoala", "DancingBear", "RunningFox",
	"HoppingRabbit", "GlidingOwl", "LeapingLeopard", "SoaringFalcon",
}

var rankStrengths = map[string]int{
	"A": 5, "1": 5,
	"10": 4, "2": 4,
	"K": 3, "3": 3,
	"Q": 2, "4": 2,
	"J": 1, "5": 1,
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func min4(a, b, c, d int) int {
	m := a
	if b < m { m = b }
	if c < m { m = c }
	if d < m { m = d }
	return m
}
func min5(a, b, c, d, e int) int {
	m := a
	if b < m { m = b }
	if c < m { m = c }
	if d < m { m = d }
	if e < m { m = e }
	return m
}

func EvaluateMeld(hand []Card, trumpSuit string, isNumberTheme bool) int {
	var spades, diamonds, clubs, hearts string
	var ace, ten, king, queen, jack string

	if isNumberTheme {
		spades = "Red"
		diamonds = "Blue"
		clubs = "Yellow"
		hearts = "Green"
		ace = "1"
		ten = "2"
		king = "3"
		queen = "4"
		jack = "5"
	} else {
		spades = "Spades"
		diamonds = "Diamonds"
		clubs = "Clubs"
		hearts = "Hearts"
		ace = "A"
		ten = "10"
		king = "K"
		queen = "Q"
		jack = "J"
	}

	suits := []string{spades, diamonds, clubs, hearts}

	// Count cards by Suit and Rank
	counts := make(map[string]map[string]int)
	for _, s := range suits {
		counts[s] = make(map[string]int)
	}
	for _, c := range hand {
		if _, ok := counts[c.Suit]; ok {
			counts[c.Suit][c.Rank]++
		}
	}

	totalMeld := 0

	// --- Class A ---
	// 1. Run in Trump
	trumpCounts := counts[trumpSuit]
	rA := trumpCounts[ace]
	r10 := trumpCounts[ten]
	rK := trumpCounts[king]
	rQ := trumpCounts[queen]
	rJ := trumpCounts[jack]

	runs := min5(rA, r10, rK, rQ, rJ)
	if runs > 0 {
		runScores := []int{0, 15, 150, 225, 300}
		if runs < len(runScores) {
			totalMeld += runScores[runs]
		} else {
			totalMeld += runScores[len(runScores)-1]
		}
	}

	// 2. Royal Marriage
	royalMarriages := min2(rK-runs, rQ-runs)
	if royalMarriages > 0 {
		rmScores := []int{0, 4, 8, 12, 16}
		if royalMarriages < len(rmScores) {
			totalMeld += rmScores[royalMarriages]
		} else {
			totalMeld += rmScores[len(rmScores)-1]
		}
	}

	// 3. Common Marriages
	for _, s := range suits {
		if s == trumpSuit {
			continue
		}
		sK := counts[s][king]
		sQ := counts[s][queen]
		cm := min2(sK, sQ)
		if cm > 0 {
			cmScores := []int{0, 2, 4, 6, 8}
			if cm < len(cmScores) {
				totalMeld += cmScores[cm]
			} else {
				totalMeld += cmScores[len(cmScores)-1]
			}
		}
	}

	// --- Class B ---
	// 1. Aces Around
	aces := min4(counts[spades][ace], counts[diamonds][ace], counts[clubs][ace], counts[hearts][ace])
	if aces > 0 {
		scores := []int{0, 10, 100, 150, 200}
		if aces < len(scores) {
			totalMeld += scores[aces]
		} else {
			totalMeld += scores[len(scores)-1]
		}
	}

	// 2. Kings Around
	kings := min4(counts[spades][king], counts[diamonds][king], counts[clubs][king], counts[hearts][king])
	if kings > 0 {
		scores := []int{0, 8, 80, 120, 160}
		if kings < len(scores) {
			totalMeld += scores[kings]
		} else {
			totalMeld += scores[len(scores)-1]
		}
	}

	// 3. Queens Around
	queens := min4(counts[spades][queen], counts[diamonds][queen], counts[clubs][queen], counts[hearts][queen])
	if queens > 0 {
		scores := []int{0, 6, 60, 90, 120}
		if queens < len(scores) {
			totalMeld += scores[queens]
		} else {
			totalMeld += scores[len(scores)-1]
		}
	}

	// 4. Jacks Around
	jacks := min4(counts[spades][jack], counts[diamonds][jack], counts[clubs][jack], counts[hearts][jack])
	if jacks > 0 {
		scores := []int{0, 4, 40, 60, 80}
		if jacks < len(scores) {
			totalMeld += scores[jacks]
		} else {
			totalMeld += scores[len(scores)-1]
		}
	}

	// --- Class C ---
	// Pinochle
	pinochles := min2(counts[spades][queen], counts[diamonds][jack])
	if pinochles > 0 {
		scores := []int{0, 4, 30, 90, 270}
		if pinochles < len(scores) {
			totalMeld += scores[pinochles]
		} else {
			totalMeld += scores[len(scores)-1]
		}
	}

	return totalMeld
}

func SortCards(hand []Card, isNumberTheme bool) {
	var spades, diamonds, clubs, hearts string
	var ace, ten, king, queen, jack string

	if isNumberTheme {
		spades = "Red"
		diamonds = "Blue"
		clubs = "Yellow"
		hearts = "Green"
		ace = "1"
		ten = "2"
		king = "3"
		queen = "4"
		jack = "5"
	} else {
		spades = "Spades"
		diamonds = "Diamonds"
		clubs = "Clubs"
		hearts = "Hearts"
		ace = "A"
		ten = "10"
		king = "K"
		queen = "Q"
		jack = "J"
	}

	suitOrder := map[string]int{
		spades:   0,
		diamonds: 1,
		clubs:    2,
		hearts:   3,
	}

	rankOrder := map[string]int{
		ace:   0,
		ten:   1,
		king:  2,
		queen: 3,
		jack:  4,
	}

	for i := 0; i < len(hand)-1; i++ {
		for j := i + 1; j < len(hand); j++ {
			sI := suitOrder[hand[i].Suit]
			sJ := suitOrder[hand[j].Suit]
			if sI > sJ || (sI == sJ && rankOrder[hand[i].Rank] > rankOrder[hand[j].Rank]) {
				hand[i], hand[j] = hand[j], hand[i]
			}
		}
	}
}

func DealHand(maxPlayers int, isNumberTheme bool) [][]Card {
	var suits []string
	var ranks []string

	if isNumberTheme {
		suits = []string{"Red", "Blue", "Yellow", "Green"}
		ranks = []string{"1", "2", "3", "4", "5"}
	} else {
		suits = []string{"Spades", "Diamonds", "Clubs", "Hearts"}
		ranks = []string{"A", "10", "K", "Q", "J"}
	}

	copies := 4
	if maxPlayers == 6 {
		copies = 6
	}

	var deck []Card
	for _, s := range suits {
		for _, r := range ranks {
			for i := 0; i < copies; i++ {
				deck = append(deck, Card{Suit: s, Rank: r})
			}
		}
	}

	r := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})

	hands := make([][]Card, maxPlayers)
	for i := 0; i < maxPlayers; i++ {
		hands[i] = deck[i*20 : (i+1)*20]
		SortCards(hands[i], isNumberTheme)
	}

	return hands
}

func hasMarriage(hand []Card, suit string, isNumberTheme bool) bool {
	var kingRank, queenRank string
	if isNumberTheme {
		kingRank = "3"
		queenRank = "4"
	} else {
		kingRank = "K"
		queenRank = "Q"
	}

	hasKing := false
	hasQueen := false
	for _, c := range hand {
		if c.Suit == suit {
			if c.Rank == kingRank {
				hasKing = true
			}
			if c.Rank == queenRank {
				hasQueen = true
			}
		}
	}
	return hasKing && hasQueen
}

func hasAnyMarriage(hand []Card, isNumberTheme bool) bool {
	var suits []string
	if isNumberTheme {
		suits = []string{"Red", "Blue", "Yellow", "Green"}
	} else {
		suits = []string{"Spades", "Diamonds", "Clubs", "Hearts"}
	}
	for _, s := range suits {
		if hasMarriage(hand, s, isNumberTheme) {
			return true
		}
	}
	return false
}

func Beats(cardA, cardB Card, trumpSuit, ledSuit string) bool {
	if cardA.Suit == cardB.Suit {
		return rankStrengths[cardA.Rank] > rankStrengths[cardB.Rank]
	}
	if cardA.Suit == trumpSuit {
		return true
	}
	if cardB.Suit == trumpSuit {
		return false
	}
	if cardA.Suit == ledSuit {
		return true
	}
	if cardB.Suit == ledSuit {
		return false
	}
	return false
}

func GetValidCards(hand []Card, currentTrick []TrickCard, trumpSuit string, isNumberTheme bool) []Card {
	if len(currentTrick) == 0 {
		return hand
	}

	ledSuit := currentTrick[0].Card.Suit

	highestCard := currentTrick[0].Card
	for _, tc := range currentTrick {
		if Beats(tc.Card, highestCard, trumpSuit, ledSuit) {
			highestCard = tc.Card
		}
	}

	var followCards []Card
	for _, c := range hand {
		if c.Suit == ledSuit {
			followCards = append(followCards, c)
		}
	}

	if len(followCards) > 0 {
		var beatingCards []Card
		for _, c := range followCards {
			if Beats(c, highestCard, trumpSuit, ledSuit) {
				beatingCards = append(beatingCards, c)
			}
		}
		if len(beatingCards) > 0 {
			return beatingCards
		}
		return followCards
	}

	var trumpCards []Card
	for _, c := range hand {
		if c.Suit == trumpSuit {
			trumpCards = append(trumpCards, c)
		}
	}

	if len(trumpCards) > 0 {
		var beatingTrump []Card
		if highestCard.Suit == trumpSuit {
			for _, c := range trumpCards {
				if Beats(c, highestCard, trumpSuit, ledSuit) {
					beatingTrump = append(beatingTrump, c)
				}
			}
			if len(beatingTrump) > 0 {
				return beatingTrump
			}
		}
		return trumpCards
	}

	return hand
}

// TeamStats stores the scorecard details for a team in a single round.
type TeamStats struct {
	Meld       int  `json:"meld" yaml:"meld"`
	Tricks     int  `json:"tricks" yaml:"tricks"`
	RoundScore int  `json:"roundScore" yaml:"roundScore"`
	SavedMeld  bool `json:"savedMeld" yaml:"savedMeld"`
}

// Round records the play statistics for a single round of a game.
type Round struct {
	RoundNumber   int                  `json:"roundNumber" yaml:"roundNumber"`
	BiddingPlayer string               `json:"biddingPlayer,omitempty" yaml:"biddingPlayer,omitempty"`
	BiddingTeam   string               `json:"biddingTeam" yaml:"biddingTeam"`
	BidAmount     int                  `json:"bidAmount" yaml:"bidAmount"`
	TrumpSuit     string               `json:"trumpSuit,omitempty" yaml:"trumpSuit,omitempty"`
	TeamStats     map[string]TeamStats `json:"teamStats" yaml:"teamStats"`
}

// Game represents a full Pinochle game scorecard and history.
type Game struct {
	ID            int            `json:"id" yaml:"id"`
	SessionID     string         `json:"sessionId" yaml:"sessionId"`
	Timestamp     time.Time      `json:"timestamp" yaml:"timestamp"`
	Status        string         `json:"status" yaml:"status"` // "active" or "completed"
	Teams         []string       `json:"teams" yaml:"teams"`
	Players       []string       `json:"players,omitempty" yaml:"players,omitempty"`
	Rounds        []Round        `json:"rounds" yaml:"rounds"`
	CurrentTotals map[string]int `json:"currentTotals" yaml:"currentTotals"`
}

func generateSessionID() string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("P%d", time.Now().UnixNano()%10000)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// AppState is the JSON response representing the full state of the application.
type AppState struct {
	History    []Game `json:"history"`
	ActiveGame *Game  `json:"activeGame"`
}

// CalculateRound applies the 6-player Pinochle variant scoring rules.
func CalculateRound(biddingTeam string, bidAmount int, melds map[string]int, tricks map[string]int, teams []string) (Round, error) {
	// 1. Validations
	minBid := 60
	if len(teams) == 2 {
		minBid = 50
	}
	if bidAmount < minBid {
		return Round{}, fmt.Errorf("bid amount must be at least %d", minBid)
	}

	// Validate teams exist
	var teamSet = make(map[string]bool)
	for _, t := range teams {
		teamSet[t] = true
	}
	if !teamSet[biddingTeam] {
		return Round{}, fmt.Errorf("bidding team %q is not in this game", biddingTeam)
	}

	// Verify tricks sum
	tricksSum := 0
	for _, t := range teams {
		tricksSum += tricks[t]
	}
	expectedTricks := 75
	if len(teams) == 2 {
		expectedTricks = 50
	}
	if tricksSum != expectedTricks {
		// Allow tricksSum == 0 as a special case for aborted rounds (e.g. no trump marriage)
		if tricksSum != 0 {
			return Round{}, fmt.Errorf("trick points must sum to exactly %d (got %d)", expectedTricks, tricksSum)
		}
	}

	// 2. Calculations
	teamStats := make(map[string]TeamStats)
	for _, t := range teams {
		meldVal := melds[t]
		// Partnership must have at least 20 meld points to claim/put down any meld
		if meldVal < 20 {
			meldVal = 0
		}
		tricksVal := tricks[t]
		var roundScore int
		var savedMeld bool

		if t == biddingTeam {
			// Bidding Team Rule:
			// Must earn at least (Meld + Tricks >= Bid)
			if meldVal+tricksVal >= bidAmount {
				roundScore = meldVal + tricksVal
				savedMeld = true
			} else {
				// Failed bid: round score is negative Bid Amount, meld and tricks are discarded
				roundScore = -bidAmount
				savedMeld = false
			}
		} else {
			// Non-Bidding Team Rule (10-Point Save):
			// Must score at least 10 trick points to save meld.
			if tricksVal >= 10 {
				roundScore = meldVal + tricksVal
				savedMeld = true
			} else {
				// Failed to save meld: meld is wiped, only score tricks
				roundScore = tricksVal
				savedMeld = false
			}
		}

		teamStats[t] = TeamStats{
			Meld:       meldVal,
			Tricks:     tricksVal,
			RoundScore: roundScore,
			SavedMeld:  savedMeld,
		}
	}

	return Round{
		BiddingTeam: biddingTeam,
		BidAmount:   bidAmount,
		TeamStats:   teamStats,
	}, nil
}

// GameManager manages games history storage and state thread-safely.
type GameManager struct {
	sync.Mutex
	historyPath string
	history     []Game
	activeGame  *Game
}

// NewGameManager initializes and loads the local database from games_history.yaml.
func NewGameManager(path string) (*GameManager, error) {
	mgr := &GameManager{
		historyPath: path,
		history:     make([]Game, 0),
	}

	if err := mgr.loadHistory(); err != nil {
		return nil, err
	}

	return mgr, nil
}

// Load database from file. If file does not exist, it initializes a clean empty file.
func (m *GameManager) loadHistory() error {
	file, err := os.Open(m.historyPath)
	if os.IsNotExist(err) {
		m.history = make([]Game, 0)
		return m.saveHistory()
	} else if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer file.Close()

	var games []Game
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&games); err != nil {
		// Clean start if file is empty or corrupted
		log.Printf("YAML decode failed or file is empty; starting with blank history: %v", err)
		m.history = make([]Game, 0)
		return m.saveHistory()
	}

	m.history = games

	// Scan for active game
	hasEmptySessionID := false
	for i, g := range m.history {
		if g.Status == "active" {
			m.activeGame = &m.history[i]
			if g.SessionID == "" {
				m.history[i].SessionID = generateSessionID()
				hasEmptySessionID = true
			}
			break
		}
	}
	if hasEmptySessionID {
		_ = m.saveHistory()
	}

	return nil
}

// Save history atomically using temp file write + rename pattern
func (m *GameManager) saveHistory() error {
	tempFile := m.historyPath + ".tmp"
	file, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temporary write file: %w", err)
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	if err := encoder.Encode(m.history); err != nil {
		return fmt.Errorf("failed to encode history data: %w", err)
	}
	file.Close()

	if err := os.Rename(tempFile, m.historyPath); err != nil {
		return fmt.Errorf("failed to atomize history file commit: %w", err)
	}

	return nil
}

// StartGame starts a new active game session.
func (m *GameManager) StartGame(teams []string, players []string) (*Game, error) {
	m.Lock()
	defer m.Unlock()

	if m.activeGame != nil {
		return nil, fmt.Errorf("an active game already exists")
	}
	if len(teams) != 2 && len(teams) != 3 {
		return nil, fmt.Errorf("either 2 or 3 teams are required")
	}
	for i, t := range teams {
		if t == "" {
			return nil, fmt.Errorf("team name %d cannot be empty", i+1)
		}
	}
	if len(players) != 4 && len(players) != 6 {
		return nil, fmt.Errorf("either 4 or 6 players are required")
	}
	if len(players) != len(teams)*2 {
		return nil, fmt.Errorf("number of players must be twice the number of teams")
	}
	for i, p := range players {
		if p == "" {
			return nil, fmt.Errorf("player name %d cannot be empty", i+1)
		}
	}

	nextID := 1
	for _, g := range m.history {
		if g.ID >= nextID {
			nextID = g.ID + 1
		}
	}

	game := Game{
		ID:            nextID,
		SessionID:     generateSessionID(),
		Timestamp:     time.Now(),
		Status:        "active",
		Teams:         teams,
		Players:       players,
		Rounds:        make([]Round, 0),
		CurrentTotals: make(map[string]int),
	}
	for _, t := range teams {
		game.CurrentTotals[t] = 0
	}

	m.history = append(m.history, game)
	m.activeGame = &m.history[len(m.history)-1]

	if err := m.saveHistory(); err != nil {
		// Rollback in memory
		m.history = m.history[:len(m.history)-1]
		m.activeGame = nil
		return nil, err
	}

	return m.activeGame, nil
}

type SubmitRoundPayload struct {
	SessionID     string         `json:"sessionId"`
	BiddingPlayer string         `json:"biddingPlayer"`
	BiddingTeam   string         `json:"biddingTeam"`
	BidAmount     int            `json:"bidAmount"`
	TrumpSuit     string         `json:"trumpSuit"`
	Melds         map[string]int `json:"melds"`
	Tricks        map[string]int `json:"tricks"`
}

// SubmitRound processes scoring calculations and appends a round card to the active game.
func (m *GameManager) SubmitRound(p SubmitRoundPayload) (*Game, error) {
	m.Lock()
	defer m.Unlock()

	var targetGame *Game
	if p.SessionID != "" {
		for i, g := range m.history {
			if g.SessionID == p.SessionID && g.Status == "active" {
				targetGame = &m.history[i]
				break
			}
		}
		if targetGame == nil {
			return nil, fmt.Errorf("active game with session ID %q not found", p.SessionID)
		}
	} else {
		targetGame = m.activeGame
		if targetGame == nil {
			return nil, fmt.Errorf("no active game exists")
		}
	}

	// Resolve bidding team from player name if it's not provided or to ensure validity
	biddingTeam := p.BiddingTeam
	if biddingTeam == "" && p.BiddingPlayer != "" {
		playerIdx := -1
		for i, pName := range targetGame.Players {
			if pName == p.BiddingPlayer {
				playerIdx = i
				break
			}
		}
		if playerIdx >= 0 {
			biddingTeam = targetGame.Teams[playerIdx/2]
		}
	}

	roundNum := len(targetGame.Rounds) + 1
	round, err := CalculateRound(biddingTeam, p.BidAmount, p.Melds, p.Tricks, targetGame.Teams)
	if err != nil {
		return nil, err
	}
	round.RoundNumber = roundNum
	round.BiddingPlayer = p.BiddingPlayer
	round.TrumpSuit = p.TrumpSuit

	targetGame.Rounds = append(targetGame.Rounds, round)

	// Apply round score to cumulative totals
	for _, t := range targetGame.Teams {
		targetGame.CurrentTotals[t] += round.TeamStats[t].RoundScore
	}

	if err := m.saveHistory(); err != nil {
		// Rollback on write failure
		targetGame.Rounds = targetGame.Rounds[:len(targetGame.Rounds)-1]
		for _, t := range targetGame.Teams {
			targetGame.CurrentTotals[t] -= round.TeamStats[t].RoundScore
		}
		return nil, err
	}

	return targetGame, nil
}

// FinalizeGame completes the active game, archiving it.
func (m *GameManager) FinalizeGame(sessionID string) (AppState, error) {
	m.Lock()
	defer m.Unlock()

	var targetGame *Game
	if sessionID != "" {
		for i, g := range m.history {
			if g.SessionID == sessionID && g.Status == "active" {
				targetGame = &m.history[i]
				break
			}
		}
		if targetGame == nil {
			return AppState{}, fmt.Errorf("active game with session ID %q not found", sessionID)
		}
	} else {
		targetGame = m.activeGame
		if targetGame == nil {
			return AppState{}, fmt.Errorf("no active game to finalize")
		}
	}

	targetGame.Status = "completed"
	targetGame.Timestamp = time.Now()

	if m.activeGame != nil && m.activeGame.ID == targetGame.ID {
		m.activeGame = nil
	}

	if err := m.saveHistory(); err != nil {
		return AppState{}, err
	}

	return AppState{
		History:    m.history,
		ActiveGame: nil,
	}, nil
}

// CancelGame completely aborts and removes the current active game.
func (m *GameManager) CancelGame(sessionID string) (AppState, error) {
	m.Lock()
	defer m.Unlock()

	var targetGame *Game
	if sessionID != "" {
		for i, g := range m.history {
			if g.SessionID == sessionID && g.Status == "active" {
				targetGame = &m.history[i]
				break
			}
		}
		if targetGame == nil {
			return AppState{}, fmt.Errorf("active game with session ID %q not found", sessionID)
		}
	} else {
		targetGame = m.activeGame
		if targetGame == nil {
			return AppState{}, fmt.Errorf("no active game to abort")
		}
	}

	activeID := targetGame.ID
	var newHistory []Game
	for _, g := range m.history {
		if g.ID != activeID {
			newHistory = append(newHistory, g)
		}
	}
	m.history = newHistory

	if m.activeGame != nil && m.activeGame.ID == activeID {
		m.activeGame = nil
	}

	if err := m.saveHistory(); err != nil {
		return AppState{}, err
	}

	return AppState{
		History:    m.history,
		ActiveGame: nil,
	}, nil
}

// GetAppState returns the current frontend payload state.
func (m *GameManager) GetAppState() AppState {
	m.Lock()
	defer m.Unlock()
	return AppState{
		History:    m.history,
		ActiveGame: m.activeGame,
	}
}

// GetAppStateForSession returns the frontend payload state for a specific Session ID.
func (m *GameManager) GetAppStateForSession(sessionID string) AppState {
	m.Lock()
	defer m.Unlock()

	var targetGame *Game
	if sessionID != "" {
		for i, g := range m.history {
			if g.SessionID == sessionID && g.Status == "active" {
				targetGame = &m.history[i]
				break
			}
		}
	}

	if targetGame == nil {
		targetGame = m.activeGame
	}

	return AppState{
		History:    m.history,
		ActiveGame: targetGame,
	}
}

// --- Online Multiplayer HTTP Handlers ---

type LobbyGame struct {
	SessionID  string   `json:"sessionId"`
	HostName   string   `json:"hostName"`
	Players    []string `json:"players"`
	MaxPlayers int      `json:"maxPlayers"`
	Status     string   `json:"status"`
	DeckTheme  string   `json:"deckTheme"`
}

func handleLobbyGames(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	onlineGamesMu.Lock()
	defer onlineGamesMu.Unlock()

	var games []LobbyGame
	for _, g := range onlineGames {
		players := make([]string, len(g.Players))
		for i, p := range g.Players {
			players[i] = p.Name
		}
		games = append(games, LobbyGame{
			SessionID:  g.SessionID,
			HostName:   g.HostName,
			Players:    players,
			MaxPlayers: g.MaxPlayers,
			Status:     g.Status,
			DeckTheme:  g.DeckTheme,
		})
	}
	// Return empty array instead of null
	if games == nil {
		games = []LobbyGame{}
	}
	json.NewEncoder(w).Encode(games)
}

func handleCreateOnlineGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		HostName       string   `json:"hostName"`
		MaxPlayers     int      `json:"maxPlayers"`
		DeckTheme      string   `json:"deckTheme"`
		DisableReadyUp bool     `json:"disableReadyUp"`
		TeamNames      []string `json:"teamNames"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	if p.HostName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Host username is required"})
		return
	}

	if p.MaxPlayers != 4 && p.MaxPlayers != 6 {
		p.MaxPlayers = 4
	}

	code := generateSessionID()
	game := &OnlineGame{
		SessionID:      code,
		Status:         "lobby",
		DeckTheme:      p.DeckTheme,
		DisableReadyUp: p.DisableReadyUp,
		HostName:       p.HostName,
		MaxPlayers:     p.MaxPlayers,
		Players:        make([]Player, p.MaxPlayers),
		Observers:      []string{},
		BiddingHistory: []BidInfo{},
		CurrentBid:     0,
		HighestBidder:  -1,
		ActiveBidder:   0,
		TrumpSuit:      "",
		MeldsDeclared:  make(map[string]int),
		CurrentTrick:   []TrickCard{},
		TrickLeader:    0,
		TricksWon:      make(map[string]int),
		TeamMeldScores: make(map[string]int),
		Scores:         make(map[string]int),
		RoundNumber:    1,
		LastActive:     time.Now(),
		TeamNames:      p.TeamNames,
	}

	// Host joins seat 0
	game.Players[0] = Player{
		Name:        p.HostName,
		IsBot:       false,
		IsConnected: true,
		Hand:        []Card{},
		Ready:       false,
		SeatIndex:   0,
	}

	// Initialize remaining slots
	for i := 1; i < p.MaxPlayers; i++ {
		game.Players[i] = Player{
			Name:        "",
			IsBot:       false,
			IsConnected: false,
			Hand:        []Card{},
			Ready:       false,
			SeatIndex:   i,
		}
	}

	teams := []string{"Team 1", "Team 2"}
	if p.MaxPlayers == 6 {
		teams = []string{"Team 1", "Team 2", "Team 3"}
	}
	for _, t := range teams {
		game.Scores[t] = 0
		game.TricksWon[t] = 0
		game.TeamMeldScores[t] = 0
	}

	onlineGamesMu.Lock()
	onlineGames[code] = game
	onlineGamesMu.Unlock()

	sendSanitizedGameState(w, game, p.HostName, true)
}

func handleJoinGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
		SeatIndex  int    `json:"seatIndex"` // -1 for observer
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	game.LastActive = time.Now()

	// Clear player from existing seats first to prevent cloning
	for i, pl := range game.Players {
		if pl.Name == p.PlayerName {
			game.Players[i] = Player{
				Name:        "",
				IsBot:       false,
				IsConnected: false,
				Hand:        []Card{},
				Ready:       false,
				SeatIndex:   i,
			}
		}
	}
	// Clear from observers
	newObservers := []string{}
	for _, obs := range game.Observers {
		if obs != p.PlayerName {
			newObservers = append(newObservers, obs)
		}
	}
	game.Observers = newObservers

	if p.SeatIndex == -1 {
		// Join as observer
		game.Observers = append(game.Observers, p.PlayerName)
	} else if p.SeatIndex >= 0 && p.SeatIndex < game.MaxPlayers {
		// Join specific seat
		if game.Players[p.SeatIndex].Name != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "Seat is already occupied"})
			return
		}
		game.Players[p.SeatIndex] = Player{
			Name:        p.PlayerName,
			IsBot:       false,
			IsConnected: true,
			Hand:        []Card{},
			Ready:       false,
			SeatIndex:   p.SeatIndex,
		}
	}

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func handleLeaveGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	game.LastActive = time.Now()

	// Remove from seat
	for i, pl := range game.Players {
		if pl.Name == p.PlayerName {
			game.Players[i] = Player{
				Name:        "",
				IsBot:       false,
				IsConnected: false,
				Hand:        []Card{},
				Ready:       false,
				SeatIndex:   i,
			}
		}
	}
	// Remove from observers
	newObservers := []string{}
	for _, obs := range game.Observers {
		if obs != p.PlayerName {
			newObservers = append(newObservers, obs)
		}
	}
	game.Observers = newObservers

	// Check if lobby is completely empty
	hasHumans := false
	for _, pl := range game.Players {
		if pl.Name != "" && !pl.IsBot {
			hasHumans = true
			break
		}
	}
	if len(game.Observers) > 0 {
		hasHumans = true
	}

	if !hasHumans {
		// Clean up lobby entirely
		onlineGamesMu.Lock()
		delete(onlineGames, p.SessionID)
		onlineGamesMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		return
	}

	// Reassign host if leaving player was host
	if game.HostName == p.PlayerName {
		for _, pl := range game.Players {
			if pl.Name != "" && !pl.IsBot {
				game.HostName = pl.Name
				break
			}
		}
	}

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func handleKickPlayer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID   string `json:"sessionId"`
		HostName    string `json:"hostName"`
		PlayerToKick string `json:"playerToKick"`
		ReplaceWith string `json:"replaceWith"` // "bot" or observer username
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	if game.HostName != p.HostName {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Only the host can kick players"})
		return
	}

	game.LastActive = time.Now()

	// Find the kicked player's seat index
	seatIndex := -1
	for i, pl := range game.Players {
		if pl.Name == p.PlayerToKick {
			seatIndex = i
			break
		}
	}

	if seatIndex == -1 {
		// Just remove from observers if they were in observers
		newObservers := []string{}
		for _, obs := range game.Observers {
			if obs != p.PlayerToKick {
				newObservers = append(newObservers, obs)
			}
		}
		game.Observers = newObservers
	} else {
		// Seat index found. Perform substitution
		if p.ReplaceWith == "bot" {
			// Find a generic animal-verb bot name
			botName := botNames[seatIndex % len(botNames)]
			game.Players[seatIndex] = Player{
				Name:        botName,
				IsBot:       true,
				IsConnected: true,
				Hand:        game.Players[seatIndex].Hand,
				Ready:       true,
				SeatIndex:   seatIndex,
			}
		} else {
			// Replace with waiting observer
			observerFound := false
			for _, obs := range game.Observers {
				if obs == p.ReplaceWith {
					observerFound = true
					break
				}
			}
			if observerFound {
				game.Players[seatIndex] = Player{
					Name:        p.ReplaceWith,
					IsBot:       false,
					IsConnected: true,
					Hand:        game.Players[seatIndex].Hand,
					Ready:       false,
					SeatIndex:   seatIndex,
				}
				// Remove observer from list
				newObservers := []string{}
				for _, obs := range game.Observers {
					if obs != p.ReplaceWith {
						newObservers = append(newObservers, obs)
					}
				}
				game.Observers = newObservers
			} else {
				// No substitution found, just leave seat empty
				game.Players[seatIndex] = Player{
					Name:        "",
					IsBot:       false,
					IsConnected: false,
					Hand:        []Card{},
					Ready:       false,
					SeatIndex:   seatIndex,
				}
			}
		}
	}

	// Trigger bot evaluation if turn advanced
	checkAndRunBots(game)

	sendSanitizedGameState(w, game, p.HostName, true)
}

func handleBid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
		Bid        int    `json:"bid"`
		Pass       bool   `json:"pass"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	game.LastActive = time.Now()

	if game.Status != "bidding" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "Bidding is not active"})
		return
	}

	if game.Players[game.ActiveBidder].Name != p.PlayerName {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "It is not your turn to bid"})
		return
	}

	minStartBid := 50
	if game.MaxPlayers == 6 {
		minStartBid = 60
	}

	if p.Pass {
		game.BiddingHistory = append(game.BiddingHistory, BidInfo{
			Player: p.PlayerName,
			Bid:    0,
			Pass:   true,
		})
	} else {
		if p.Bid < minStartBid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Bid must be at least %d", minStartBid)})
			return
		}
		if p.Bid <= game.CurrentBid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Bid must be greater than current bid"})
			return
		}
		if game.MaxPlayers == 6 {
			if p.Bid <= 70 {
				if game.CurrentBid > 0 {
					diff := p.Bid - game.CurrentBid
					if diff < 1 || diff > 10 {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						json.NewEncoder(w).Encode(map[string]string{"error": "Bids below 70 must increase by 1 to 10 points"})
						return
					}
				} else {
					if p.Bid < minStartBid || p.Bid > 70 {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("First bid below 70 must be between %d and 70", minStartBid)})
						return
					}
				}
			} else {
				if p.Bid%5 != 0 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]string{"error": "Bids above 70 must be in multiples of 5"})
					return
				}
				if game.CurrentBid >= 70 && (p.Bid-game.CurrentBid)%5 != 0 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]string{"error": "Bids above 70 must increase by multiples of 5"})
					return
				}
			}
		} else {
			if p.Bid%5 != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Bids must be in multiples of 5"})
				return
			}
			if game.CurrentBid > 0 && (p.Bid-game.CurrentBid)%5 != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Bids must increase by multiples of 5"})
				return
			}
		}

		game.CurrentBid = p.Bid
		game.HighestBidder = game.ActiveBidder
		game.BiddingHistory = append(game.BiddingHistory, BidInfo{
			Player: p.PlayerName,
			Bid:    p.Bid,
			Pass:   false,
		})
	}

	advanceBidder(game)

	// Run recursive bot bidders
	checkAndRunBots(game)

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func handleDeclareTrump(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
		TrumpSuit  string `json:"trumpSuit"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	game.LastActive = time.Now()

	if game.Status != "meld" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "Meld trump declaration is not active"})
		return
	}

	if game.Players[game.HighestBidder].Name != p.PlayerName {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Only the highest bidder can declare trump"})
		return
	}

	isNumberTheme := game.DeckTheme == "number"
	hasSelectedMarriage := hasMarriage(game.Players[game.HighestBidder].Hand, p.TrumpSuit, isNumberTheme)
	
	if !hasSelectedMarriage {
		if hasAnyMarriage(game.Players[game.HighestBidder].Hand, isNumberTheme) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("You do not hold a marriage (King & Queen) in %s. You must declare a suit where you have a marriage.", p.TrumpSuit)})
			return
		}
		
		// If they have no marriage at all, they go set immediately and the round is aborted!
		game.TrumpSuit = p.TrumpSuit
		teams := []string{"Team 1", "Team 2"}
		if game.MaxPlayers == 6 {
			teams = []string{"Team 1", "Team 2", "Team 3"}
		}
		game.TeamMeldScores = make(map[string]int)
		game.TricksWon = make(map[string]int)
		for _, t := range teams {
			game.TeamMeldScores[t] = 0
			game.TricksWon[t] = 0
		}
		finishRound(game)
		sendSanitizedGameState(w, game, p.PlayerName, true)
		return
	}

	game.TrumpSuit = p.TrumpSuit
	game.Status = "meld_show"
	game.MeldShowStarted = time.Now()

	// Calculate and store team meld scores and player meld cards
	populateMeldScoresAndCards(game)

	// Set Trick leader to highest bidder
	game.TrickLeader = game.HighestBidder
	game.CurrentTrick = []TrickCard{}

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func handlePlayCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
		Card       Card   `json:"card"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	game.LastActive = time.Now()

	if game.Status != "playing" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "Trick playing is not active"})
		return
	}

	// Find active player seat index
	activeIdx := (game.TrickLeader + len(game.CurrentTrick)) % game.MaxPlayers
	if game.Players[activeIdx].Name != p.PlayerName {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "It is not your turn to play"})
		return
	}

	// Clear full trick if next play leads
	if len(game.CurrentTrick) == game.MaxPlayers {
		game.CurrentTrick = nil
	}

	// Validate card played
	valid := GetValidCards(game.Players[activeIdx].Hand, game.CurrentTrick, game.TrumpSuit, game.DeckTheme == "number")
	cardValid := false
	for _, vc := range valid {
		if vc.Suit == p.Card.Suit && vc.Rank == p.Card.Rank {
			cardValid = true
			break
		}
	}
	if !cardValid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "You must follow suit and trumping rules"})
		return
	}

	// Play card
	playCardInGame(game, activeIdx, p.Card)

	// Trigger next bot plays recursively
	checkAndRunBots(game)

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func handleReadyUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
		Ready      bool   `json:"ready"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	game.LastActive = time.Now()

	// Update player ready
	for i, pl := range game.Players {
		if pl.Name == p.PlayerName {
			game.Players[i].Ready = p.Ready
		}
	}

	// If Host disabled ready up requirement, or if ALL humans are ready
	allReady := true
	if game.DisableReadyUp {
		// Only host needs to be ready to transition
		for _, pl := range game.Players {
			if pl.Name == game.HostName && !pl.Ready {
				allReady = false
			}
		}
	} else {
		for _, pl := range game.Players {
			if pl.Name != "" && !pl.IsBot && !pl.Ready {
				allReady = false
			}
		}
	}

	if allReady {
		if game.Status == "lobby" {
			// Verify minimum humans: must have at least one human player!
			hasHumans := false
			for _, pl := range game.Players {
				if pl.Name != "" && !pl.IsBot {
					hasHumans = true
					break
				}
			}
			if !hasHumans {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Lobby must have at least one active human player to start"})
				return
			}

			// Start Game!
			// Fill empty slots with bots
			for i := 0; i < game.MaxPlayers; i++ {
				if game.Players[i].Name == "" {
					botName := botNames[i%len(botNames)]
					game.Players[i] = Player{
						Name:        botName,
						IsBot:       true,
						IsConnected: true,
						Hand:        []Card{},
						Ready:       true,
						SeatIndex:   i,
					}
				}
			}

			// Deal cards
			game.Players = setupPlayersHands(game)
			game.Status = "bidding"
			game.ActiveBidder = 0
			game.CurrentBid = 0
			game.BiddingHistory = []BidInfo{}
			game.TrumpSuit = ""
			game.CurrentTrick = []TrickCard{}
			game.LastCompletedTrick = []TrickCard{}
			game.LastTrickWinner = ""

		} else if game.Status == "summary" {
			// Check if any team has won (reached 500 points)
			gameWon := false
			for _, score := range game.Scores {
				if score >= 500 {
					gameWon = true
					break
				}
			}

			if gameWon {
				// Finalize game and add to History database!
				game.Status = "completed"
				onlineGamesMu.Lock()
				delete(onlineGames, p.SessionID)
				onlineGamesMu.Unlock()

				// Add to history
				// Prepare the teams/players payload matching scorecard Game struct
				tNames := []string{"Team 1", "Team 2"}
				if game.MaxPlayers == 6 {
					tNames = []string{"Team 1", "Team 2", "Team 3"}
				}

				playerNames := make([]string, game.MaxPlayers)
				for idx, pl := range game.Players {
					playerNames[idx] = pl.Name
				}

				// Finalize scorecard into manager's database list
				// First load history and add a completed Game
				// We can initialize Game
				mgr, _ := NewGameManager(HistoryFileName)
				mgr.StartGame(tNames, playerNames)
				mgr.activeGame.SessionID = game.SessionID
				mgr.activeGame.Rounds = game.RoundsCompleted
				mgr.activeGame.CurrentTotals = game.Scores
				mgr.activeGame.Status = "completed"
				mgr.saveHistory()
				
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
				return
			} else {
				// Start next round!
				game.RoundNumber++
				game.Players = setupPlayersHands(game)
				game.Status = "bidding"
				game.BiddingHistory = []BidInfo{}
				game.CurrentBid = 0
				game.HighestBidder = -1
				game.ActiveBidder = (game.RoundNumber - 1) % game.MaxPlayers
				game.TrumpSuit = ""
				game.CurrentTrick = []TrickCard{}
				game.LastCompletedTrick = []TrickCard{}
				game.LastTrickWinner = ""
			}
		}
	}

	// Trigger bots if state transitioned
	checkAndRunBots(game)

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func handleHostSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID      string `json:"sessionId"`
		HostName       string `json:"hostName"`
		MaxPlayers     int    `json:"maxPlayers"`
		DisableReadyUp bool   `json:"disableReadyUp"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game lobby not found"})
		return
	}

	if game.HostName != p.HostName {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Only host can change lobby settings"})
		return
	}

	game.LastActive = time.Now()

	if game.Status == "lobby" {
		if p.MaxPlayers == 4 || p.MaxPlayers == 6 {
			if game.MaxPlayers != p.MaxPlayers {
				game.MaxPlayers = p.MaxPlayers
				game.Players = make([]Player, p.MaxPlayers)
				// Re-initialize seats, putting Host back in seat 0
				game.Players[0] = Player{
					Name:        p.HostName,
					IsBot:       false,
					IsConnected: true,
					Hand:        []Card{},
					Ready:       false,
					SeatIndex:   0,
				}
				for i := 1; i < p.MaxPlayers; i++ {
					game.Players[i] = Player{
						Name:        "",
						IsBot:       false,
						IsConnected: false,
						Hand:        []Card{},
						Ready:       false,
						SeatIndex:   i,
					}
				}

				// Reset scores/wins maps
				teams := []string{"Team 1", "Team 2"}
				if p.MaxPlayers == 6 {
					teams = []string{"Team 1", "Team 2", "Team 3"}
				}
				game.Scores = make(map[string]int)
				game.TricksWon = make(map[string]int)
				game.TeamMeldScores = make(map[string]int)
				for _, t := range teams {
					game.Scores[t] = 0
					game.TricksWon[t] = 0
					game.TeamMeldScores[t] = 0
				}
			}
		}
	}

	game.DisableReadyUp = p.DisableReadyUp

	sendSanitizedGameState(w, game, p.HostName, true)
}

func handleOnlineState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	playerName := r.URL.Query().Get("playerName")

	onlineGamesMu.Lock()
	game, ok := onlineGames[sessionID]
	if ok {
		game.LastActive = time.Now()

		// Mark connected
		for i, pl := range game.Players {
			if pl.Name == playerName {
				game.Players[i].IsConnected = true
			}
		}

		// Auto transition from meld_show to playing after 20 seconds for multi-human games
		if game.Status == "meld_show" {
			botsCount := 0
			for _, p := range game.Players {
				if p.IsBot {
					botsCount++
				}
			}
			if botsCount < game.MaxPlayers-1 {
				if !game.MeldShowStarted.IsZero() && time.Since(game.MeldShowStarted) >= 20*time.Second {
					game.Status = "playing"
					checkAndRunBots(game)
				}
			}
		}

		// Check if it is a bot's turn and trigger if so
		isBotTurn := false
		if game.Status == "bidding" {
			botIdx := game.ActiveBidder
			if botIdx >= 0 && botIdx < game.MaxPlayers && game.Players[botIdx].IsBot && game.Players[botIdx].Name != "" {
				isBotTurn = true
			}
		} else if game.Status == "meld" {
			botIdx := game.HighestBidder
			if botIdx >= 0 && botIdx < game.MaxPlayers && game.Players[botIdx].IsBot && game.Players[botIdx].Name != "" {
				isBotTurn = true
			}
		} else if game.Status == "playing" {
			activeIdx := (game.TrickLeader + len(game.CurrentTrick)) % game.MaxPlayers
			if activeIdx >= 0 && activeIdx < game.MaxPlayers && game.Players[activeIdx].IsBot && game.Players[activeIdx].Name != "" {
				isBotTurn = true
			}
		}
		if isBotTurn {
			checkAndRunBots(game)
		}
	}
	onlineGamesMu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game not found"})
		return
	}

	sendSanitizedGameState(w, game, playerName, false)
}

func handleAcknowledgeMeld(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Payload struct {
		SessionID  string `json:"sessionId"`
		PlayerName string `json:"playerName"`
	}

	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	onlineGamesMu.Lock()
	game, ok := onlineGames[p.SessionID]
	if !ok {
		onlineGamesMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Game not found"})
		return
	}

	if game.Status != "meld_show" {
		onlineGamesMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "Meld acknowledgment is not active"})
		return
	}

	// Transition to playing and trigger bots!
	game.Status = "playing"
	game.LastActive = time.Now()
	checkAndRunBots(game)

	onlineGamesMu.Unlock()

	sendSanitizedGameState(w, game, p.PlayerName, true)
}

func sendSanitizedGameState(w http.ResponseWriter, game *OnlineGame, playerName string, saveState bool) {
	if saveState {
		saveActiveGames()
	}

	w.Header().Set("Content-Type", "application/json")

	// Determine if requester is observer
	isObserver := true
	for _, pl := range game.Players {
		if pl.Name == playerName && !pl.IsBot {
			isObserver = false
			break
		}
	}

	// Copy game struct to sanitize private hands
	sanitized := *game
	sanitized.Players = make([]Player, len(game.Players))
	for i, p := range game.Players {
		sp := p
		if isObserver || sp.Name != playerName {
			if len(sp.Hand) > 0 {
				dummyHand := make([]Card, len(sp.Hand))
				for dIdx := range dummyHand {
					dummyHand[dIdx] = Card{Suit: "", Rank: ""}
				}
				sp.Hand = dummyHand
			} else {
				sp.Hand = nil
			}
		}
		sanitized.Players[i] = sp
	}

	json.NewEncoder(w).Encode(sanitized)
}

func isCounterCard(c Card) bool {
	return c.Rank == "A" || c.Rank == "10" || c.Rank == "K" || c.Rank == "1" || c.Rank == "2" || c.Rank == "3"
}

func cardStrength(c Card) int {
	return rankStrengths[c.Rank]
}

func getTeamName(playerIndex int, game *OnlineGame) string {
	var teamIdx int
	if game.MaxPlayers == 6 {
		teamIdx = playerIndex % 3
	} else {
		teamIdx = playerIndex % 2
	}
	if len(game.TeamNames) > teamIdx && game.TeamNames[teamIdx] != "" {
		return game.TeamNames[teamIdx]
	}
	return fmt.Sprintf("Team %d", teamIdx+1)
}

func setupPlayersHands(game *OnlineGame) []Player {
	game.LastCompletedTrick = nil
	game.LastTrickWinner = ""
	hands := DealHand(game.MaxPlayers, game.DeckTheme == "number")
	playersCopy := make([]Player, len(game.Players))
	for i := range game.Players {
		playersCopy[i] = game.Players[i]
		playersCopy[i].Hand = hands[i]
		playersCopy[i].MeldCards = nil // Clear meld cards from previous round
		SortCards(playersCopy[i].Hand, game.DeckTheme == "number")
	}
	return playersCopy
}

func hasPassed(game *OnlineGame, playerIndex int) bool {
	pName := game.Players[playerIndex].Name
	if pName == "" {
		return true
	}
	for i := len(game.BiddingHistory) - 1; i >= 0; i-- {
		if game.BiddingHistory[i].Player == pName {
			return game.BiddingHistory[i].Pass
		}
	}
	return false
}

func advanceBidder(game *OnlineGame) {
	nextBidder := game.ActiveBidder
	for {
		nextBidder = (nextBidder + 1) % game.MaxPlayers
		if nextBidder == game.ActiveBidder {
			break
		}
		if !hasPassed(game, nextBidder) {
			break
		}
	}

	activeCount := 0
	lastActiveIdx := -1
	for i := 0; i < game.MaxPlayers; i++ {
		if !hasPassed(game, i) {
			activeCount++
			lastActiveIdx = i
		}
	}

	if activeCount <= 1 {
		if lastActiveIdx != -1 && game.CurrentBid > 0 {
			game.HighestBidder = lastActiveIdx
			game.Status = "meld"
		} else {
			dealerIdx := ((game.RoundNumber - 1) + game.MaxPlayers - 1) % game.MaxPlayers
			minStartBid := 50
			if game.MaxPlayers == 6 {
				minStartBid = 60
			}
			game.HighestBidder = dealerIdx
			game.CurrentBid = minStartBid
			game.Status = "meld"
		}
	} else {
		game.ActiveBidder = nextBidder
	}
}

func getBotBestBid(game *OnlineGame, botIdx int) (bid int, pass bool) {
	hand := game.Players[botIdx].Hand
	isNumberTheme := (game.DeckTheme == "number")

	suits := []string{"Spades", "Diamonds", "Clubs", "Hearts"}
	if isNumberTheme {
		suits = []string{"Red", "Blue", "Yellow", "Green"}
	}

	bestMeld := 0
	for _, suit := range suits {
		mPts := EvaluateMeld(hand, suit, isNumberTheme)
		if mPts > bestMeld {
			bestMeld = mPts
		}
	}

	maxBidBonus := 20
	if game.MaxPlayers == 6 {
		maxBidBonus = 30
	}
	maxEstimatedBid := bestMeld + maxBidBonus

	minStartBid := 50
	if game.MaxPlayers == 6 {
		minStartBid = 60
	}

	nextBid := game.CurrentBid + 5
	if game.CurrentBid == 0 {
		nextBid = minStartBid
	} else if game.MaxPlayers == 6 {
		if nextBid > 70 && nextBid%5 != 0 {
			nextBid = ((nextBid / 5) + 1) * 5
		}
	}

	if nextBid <= maxEstimatedBid {
		return nextBid, false
	}
	return 0, true
}

func getBotBestTrump(game *OnlineGame, botIdx int) string {
	hand := game.Players[botIdx].Hand
	isNumberTheme := (game.DeckTheme == "number")

	suits := []string{"Spades", "Diamonds", "Clubs", "Hearts"}
	if isNumberTheme {
		suits = []string{"Red", "Blue", "Yellow", "Green"}
	}

	// Filter suits where bot has a marriage
	var marriageSuits []string
	for _, suit := range suits {
		if hasMarriage(hand, suit, isNumberTheme) {
			marriageSuits = append(marriageSuits, suit)
		}
	}

	if len(marriageSuits) > 0 {
		bestSuit := marriageSuits[0]
		bestMeld := -1
		for _, suit := range marriageSuits {
			mPts := EvaluateMeld(hand, suit, isNumberTheme)
			if mPts > bestMeld {
				bestMeld = mPts
				bestSuit = suit
			}
		}
		return bestSuit
	}

	// If no marriage at all, return default first suit
	return suits[0]
}

func selectBotCard(game *OnlineGame, botIdx int, validCards []Card) Card {
	if len(validCards) == 0 {
		return game.Players[botIdx].Hand[0]
	}

	if len(game.CurrentTrick) == 0 {
		bestCard := validCards[0]
		for _, c := range validCards {
			if cardStrength(c) > cardStrength(bestCard) {
				bestCard = c
			}
		}
		return bestCard
	}

	ledSuit := game.CurrentTrick[0].Card.Suit
	winnerIdx := -1
	for i := 0; i < len(game.CurrentTrick); i++ {
		tc := game.CurrentTrick[i]
		tcIdx := -1
		for pIdx, pl := range game.Players {
			if pl.Name == tc.Player {
				tcIdx = pIdx
				break
			}
		}
		if tcIdx == -1 {
			continue
		}

		if winnerIdx == -1 {
			winnerIdx = tcIdx
		} else {
			var winCard Card
			for _, wtc := range game.CurrentTrick {
				if wtc.Player == game.Players[winnerIdx].Name {
					winCard = wtc.Card
					break
				}
			}
			if Beats(tc.Card, winCard, game.TrumpSuit, ledSuit) {
				winnerIdx = tcIdx
			}
		}
	}

	partnerWinning := false
	if game.MaxPlayers == 6 {
		partnerWinning = (winnerIdx == (botIdx+3)%6)
	} else {
		partnerWinning = (winnerIdx == (botIdx+2)%4)
	}

	if partnerWinning {
		var bestCounter *Card
		for _, c := range validCards {
			// Do not smear with an Ace unless forced (i.e., Ace is our only choice)
			if isCounterCard(c) && c.Rank != "A" && c.Rank != "1" {
				if bestCounter == nil || cardStrength(c) > cardStrength(*bestCounter) {
					cc := c
					bestCounter = &cc
				}
			}
		}
		if bestCounter != nil {
			return *bestCounter
		}

		// If we couldn't find a non-Ace counter card, see if we have non-counter cards (Q, J) to save our Ace
		var bestNonCounter *Card
		for _, c := range validCards {
			if !isCounterCard(c) {
				if bestNonCounter == nil || cardStrength(c) < cardStrength(*bestNonCounter) {
					cc := c
					bestNonCounter = &cc
				}
			}
		}
		if bestNonCounter != nil {
			return *bestNonCounter
		}

		lowestCard := validCards[0]
		for _, c := range validCards {
			if cardStrength(c) < cardStrength(lowestCard) {
				lowestCard = c
			}
		}
		return lowestCard
	} else {
		var winCard Card
		for _, wtc := range game.CurrentTrick {
			if wtc.Player == game.Players[winnerIdx].Name {
				winCard = wtc.Card
				break
			}
		}

		var beatingCards []Card
		for _, c := range validCards {
			if Beats(c, winCard, game.TrumpSuit, ledSuit) {
				beatingCards = append(beatingCards, c)
			}
		}

		if len(beatingCards) > 0 {
			lowestBeating := beatingCards[0]
			for _, c := range beatingCards {
				if cardStrength(c) < cardStrength(lowestBeating) {
					lowestBeating = c
				}
			}
			return lowestBeating
		} else {
			lowestCard := validCards[0]
			for _, c := range validCards {
				if cardStrength(c) < cardStrength(lowestCard) {
					lowestCard = c
				}
			}
			return lowestCard
		}
	}
}

func playCardInGame(game *OnlineGame, playerIndex int, card Card) {
	game.CurrentTrick = append(game.CurrentTrick, TrickCard{
		Player: game.Players[playerIndex].Name,
		Card:   card,
	})

	hand := game.Players[playerIndex].Hand
	var newHand []Card
	if len(hand) > 0 {
		newHand = make([]Card, 0, len(hand)-1)
		for _, c := range hand {
			if c.Suit == card.Suit && c.Rank == card.Rank {
				card = Card{}
				continue
			}
			newHand = append(newHand, c)
		}
	} else {
		newHand = []Card{}
	}
	game.Players[playerIndex].Hand = newHand

	if len(game.CurrentTrick) == game.MaxPlayers {
		ledSuit := game.CurrentTrick[0].Card.Suit
		winnerIdx := -1

		for i := 0; i < game.MaxPlayers; i++ {
			tc := game.CurrentTrick[i]
			tcIdx := -1
			for pIdx, pl := range game.Players {
				if pl.Name == tc.Player {
					tcIdx = pIdx
					break
				}
			}
			if tcIdx == -1 {
				continue
			}

			if winnerIdx == -1 {
				winnerIdx = tcIdx
			} else {
				var winCard Card
				for _, wtc := range game.CurrentTrick {
					if wtc.Player == game.Players[winnerIdx].Name {
						winCard = wtc.Card
						break
					}
				}
				if Beats(tc.Card, winCard, game.TrumpSuit, ledSuit) {
					winnerIdx = tcIdx
				}
			}
		}

		// Save last completed trick details before starting next
		game.LastCompletedTrick = make([]TrickCard, len(game.CurrentTrick))
		copy(game.LastCompletedTrick, game.CurrentTrick)
		if winnerIdx >= 0 && winnerIdx < len(game.Players) {
			game.LastTrickWinner = game.Players[winnerIdx].Name
		} else {
			game.LastTrickWinner = ""
		}

		log.Printf("[DEBUG] Trick finished! Cards: %v, Led: %s", game.CurrentTrick, game.CurrentTrick[0].Card.Suit)
		log.Printf("[DEBUG] Winner: Seat %d (%s, Team: %s)", winnerIdx, game.Players[winnerIdx].Name, getTeamName(winnerIdx, game))

		pts := 0
		for _, tc := range game.CurrentTrick {
			if isCounterCard(tc.Card) {
				pts++
			}
		}

		isLastTrick := true
		for _, pl := range game.Players {
			if len(pl.Hand) > 0 {
				isLastTrick = false
				break
			}
		}
		if isLastTrick {
			if game.MaxPlayers == 6 {
				pts += 3
			} else {
				pts += 2
			}
		}

		tName := getTeamName(winnerIdx, game)
		game.TricksWon[tName] += pts
		log.Printf("[DEBUG] Added %d trick points to Team %s. New total: %d", pts, tName, game.TricksWon[tName])

		game.TrickLeader = winnerIdx

		roundOver := true
		for _, pl := range game.Players {
			if len(pl.Hand) > 0 {
				roundOver = false
				break
			}
		}

		if roundOver {
			finishRound(game)
		}
	}
}

func finishRound(game *OnlineGame) {
	teams := []string{"Team 1", "Team 2"}
	if game.MaxPlayers == 6 {
		teams = []string{"Team 1", "Team 2", "Team 3"}
	}
	biddingTeam := getTeamName(game.HighestBidder, game)

	log.Printf("[DEBUG] finishRound started for session %s (trump %s)", game.SessionID, game.TrumpSuit)
	log.Printf("[DEBUG] CurrentBid: %d by %s (Team: %s)", game.CurrentBid, game.Players[game.HighestBidder].Name, biddingTeam)
	log.Printf("[DEBUG] game.TeamMeldScores: %v, game.TricksWon: %v", game.TeamMeldScores, game.TricksWon)

	round, err := CalculateRound(biddingTeam, game.CurrentBid, game.TeamMeldScores, game.TricksWon, teams)
	if err != nil {
		log.Printf("[DEBUG] CalculateRound failed: %v", err)
	} else {
		log.Printf("[DEBUG] CalculateRound success! Round scorecard details: %+v", round)
		game.RoundsCompleted = append(game.RoundsCompleted, round)
		for _, t := range teams {
			game.Scores[t] += round.TeamStats[t].RoundScore
			log.Printf("[DEBUG] Team %s round score: %d, new total: %d", t, round.TeamStats[t].RoundScore, game.Scores[t])
		}
	}

	game.Status = "summary"

	for i := range game.Players {
		if game.Players[i].Name != "" {
			if game.Players[i].IsBot {
				game.Players[i].Ready = true
			} else {
				game.Players[i].Ready = false
			}
		}
	}
}

func checkAndRunBots(game *OnlineGame) {
	activeBotRoutinesMu.Lock()
	if activeBotRoutines[game.SessionID] {
		activeBotRoutinesMu.Unlock()
		return
	}
	activeBotRoutines[game.SessionID] = true
	activeBotRoutinesMu.Unlock()

	go func() {
		defer func() {
			activeBotRoutinesMu.Lock()
			delete(activeBotRoutines, game.SessionID)
			activeBotRoutinesMu.Unlock()
		}()
		runDelayedBots(game.SessionID)
	}()
}

func runDelayedBots(sessionID string) {
	for {
		time.Sleep(1500 * time.Millisecond)

		onlineGamesMu.Lock()
		game, ok := onlineGames[sessionID]
		if !ok {
			onlineGamesMu.Unlock()
			break
		}

		botActionRun := false

		if game.Status == "bidding" {
			botIdx := game.ActiveBidder
			if botIdx >= 0 && botIdx < game.MaxPlayers && game.Players[botIdx].IsBot && game.Players[botIdx].Name != "" {
				bid, pass := getBotBestBid(game, botIdx)
				pName := game.Players[botIdx].Name
				if pass {
					game.BiddingHistory = append(game.BiddingHistory, BidInfo{
						Player: pName,
						Bid:    0,
						Pass:   true,
					})
				} else {
					game.CurrentBid = bid
					game.HighestBidder = botIdx
					game.BiddingHistory = append(game.BiddingHistory, BidInfo{
						Player: pName,
						Bid:    bid,
						Pass:   false,
					})
				}
				advanceBidder(game)
				botActionRun = true
			}
		} else if game.Status == "meld" {
			botIdx := game.HighestBidder
			if botIdx >= 0 && botIdx < game.MaxPlayers && game.Players[botIdx].IsBot && game.Players[botIdx].Name != "" {
				trump := getBotBestTrump(game, botIdx)
				game.TrumpSuit = trump
				
				isNumberTheme := game.DeckTheme == "number"
				if !hasMarriage(game.Players[botIdx].Hand, trump, isNumberTheme) {
					// Bot has no marriage at all! Abort round and bot goes set immediately
					teams := []string{"Team 1", "Team 2"}
					if game.MaxPlayers == 6 {
						teams = []string{"Team 1", "Team 2", "Team 3"}
					}
					game.TeamMeldScores = make(map[string]int)
					game.TricksWon = make(map[string]int)
					for _, t := range teams {
						game.TeamMeldScores[t] = 0
						game.TricksWon[t] = 0
					}
					finishRound(game)
				} else {
					game.Status = "meld_show"
					game.MeldShowStarted = time.Now()
					populateMeldScoresAndCards(game)
					game.TrickLeader = game.HighestBidder
					game.CurrentTrick = []TrickCard{}
				}
				botActionRun = true
			}
		} else if game.Status == "playing" {
			if len(game.CurrentTrick) == game.MaxPlayers {
				game.CurrentTrick = nil
			}

			activeIdx := (game.TrickLeader + len(game.CurrentTrick)) % game.MaxPlayers
			if activeIdx >= 0 && activeIdx < game.MaxPlayers && game.Players[activeIdx].IsBot && game.Players[activeIdx].Name != "" {
				valid := GetValidCards(game.Players[activeIdx].Hand, game.CurrentTrick, game.TrumpSuit, game.DeckTheme == "number")
				cardToPlay := selectBotCard(game, activeIdx, valid)
				playCardInGame(game, activeIdx, cardToPlay)
				botActionRun = true
			}
		}

		if botActionRun {
			game.LastActive = time.Now()
			saveActiveGamesLocked()
		}

		onlineGamesMu.Unlock()

		if !botActionRun {
			break
		}
	}
}

func StartGameCleaner() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			onlineGamesMu.Lock()
			cleanedAny := false
			for code, game := range onlineGames {
				if time.Since(game.LastActive) > 30*time.Minute {
					log.Printf("Cleaning up inactive game session: %s", code)
					delete(onlineGames, code)
					cleanedAny = true
				}
			}
			if cleanedAny {
				saveActiveGamesLocked()
			}
			onlineGamesMu.Unlock()
		}
	}()
}

func saveActiveGamesLocked() {
	data, err := json.MarshalIndent(onlineGames, "", "  ")
	if err != nil {
		log.Printf("[ERROR] Failed to marshal active games: %v", err)
		return
	}
	err = os.WriteFile("active_games.json", data, 0644)
	if err != nil {
		log.Printf("[ERROR] Failed to write active games file: %v", err)
	}
}

func saveActiveGames() {
	onlineGamesMu.Lock()
	defer onlineGamesMu.Unlock()
	saveActiveGamesLocked()
}

func loadActiveGames() {
	onlineGamesMu.Lock()
	defer onlineGamesMu.Unlock()

	data, err := os.ReadFile("active_games.json")
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("No active games file found to resume. Starting fresh.")
			return
		}
		log.Printf("[ERROR] Failed to read active games file: %v", err)
		return
	}

	var loaded map[string]*OnlineGame
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("[ERROR] Failed to unmarshal active games: %v", err)
		return
	}

	// Restore onto onlineGames map
	onlineGames = loaded
	log.Printf("Resumed %d active online game sessions from disk.", len(onlineGames))
}

func main() {
	log.Println("Starting Pinochle Scorecard server...")

	// Initialize manager
	mgr, err := NewGameManager(HistoryFileName)
	if err != nil {
		log.Fatalf("Critical error initializing database: %v", err)
	}

	// Load active games from disk
	loadActiveGames()

	// Start clean up routine
	StartGameCleaner()

	// HTTP Routing Setup
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/state", handleGetState(mgr))
	http.HandleFunc("/api/start-game", handleStartGame(mgr))
	http.HandleFunc("/api/submit-round", handleSubmitRound(mgr))
	http.HandleFunc("/api/finalize-game", handleFinalizeGame(mgr))
	http.HandleFunc("/api/cancel-game", handleCancelGame(mgr))

	// Online Multiplayer API Routes
	http.HandleFunc("/api/lobby-games", handleLobbyGames)
	http.HandleFunc("/api/create-online-game", handleCreateOnlineGame)
	http.HandleFunc("/api/join-game", handleJoinGame)
	http.HandleFunc("/api/leave-game", handleLeaveGame)
	http.HandleFunc("/api/kick-player", handleKickPlayer)
	http.HandleFunc("/api/bid", handleBid)
	http.HandleFunc("/api/declare-trump", handleDeclareTrump)
	http.HandleFunc("/api/play-card", handlePlayCard)
	http.HandleFunc("/api/ready-up", handleReadyUp)
	http.HandleFunc("/api/host-settings", handleHostSettings)
	http.HandleFunc("/api/online-state", handleOnlineState)
	http.HandleFunc("/api/acknowledge-meld", handleAcknowledgeMeld)
	http.HandleFunc("/api/android-version", handleAndroidVersionCheck)
	http.HandleFunc("/android/app-debug.apk", handleAndroidApkDownload)

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server listening on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server startup failed: %v", err)
	}
}

// --- HTTP Route Handlers ---

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	tmplContent, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, "Failed to read embedded templates", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("index").Parse(string(tmplContent))
	if err != nil {
		http.Error(w, "Template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, nil); err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

func handleGetState(m *GameManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sessionID := r.URL.Query().Get("sessionId")
		json.NewEncoder(w).Encode(m.GetAppStateForSession(sessionID))
	}
}

func handleStartGame(m *GameManager) http.HandlerFunc {
	type RequestPayload struct {
		Teams   []string `json:"teams"`
		Players []string `json:"players"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload RequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
			return
		}

		game, err := m.StartGame(payload.Teams, payload.Players)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(game)
	}
}

func handleSubmitRound(m *GameManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload SubmitRoundPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid input formatting"})
			return
		}

		game, err := m.SubmitRound(payload)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(game)
	}
}

func handleFinalizeGame(m *GameManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.URL.Query().Get("sessionId")
		state, err := m.FinalizeGame(sessionID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}
}

func handleCancelGame(m *GameManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.URL.Query().Get("sessionId")
		state, err := m.CancelGame(sessionID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}
}

func populateMeldScoresAndCards(game *OnlineGame) {
	teams := []string{"Team 1", "Team 2"}
	if game.MaxPlayers == 6 {
		teams = []string{"Team 1", "Team 2", "Team 3"}
	}
	log.Printf("[DEBUG] populateMeldScoresAndCards started for session %s (Trump: %s, Theme: %s)", game.SessionID, game.TrumpSuit, game.DeckTheme)
	for _, t := range teams {
		game.TeamMeldScores[t] = 0
		game.TricksWon[t] = 0
	}
	
	game.MeldsDeclared = make(map[string]int)
	for idx, pl := range game.Players {
		tName := getTeamName(idx, game)
		mPts := EvaluateMeld(pl.Hand, game.TrumpSuit, game.DeckTheme == "number")
		log.Printf("[DEBUG] Player: %s (%s), Hand size: %d, Evaluated Meld Points: %d", pl.Name, tName, len(pl.Hand), mPts)
		game.TeamMeldScores[tName] += mPts
		game.MeldsDeclared[pl.Name] = mPts
		game.Players[idx].MeldCards = GetMeldCards(pl.Hand, game.TrumpSuit, game.DeckTheme == "number")
	}

	for _, t := range teams {
		rawScore := game.TeamMeldScores[t]
		if game.TeamMeldScores[t] < 20 {
			game.TeamMeldScores[t] = 0
		}
		log.Printf("[DEBUG] Team %s total meld: %d (raw: %d)", t, game.TeamMeldScores[t], rawScore)
	}
}

func GetMeldCards(hand []Card, trumpSuit string, isNumberTheme bool) []Card {
	var spades, diamonds, clubs, hearts string
	var ace, ten, king, queen, jack string

	if isNumberTheme {
		spades = "Red"
		diamonds = "Blue"
		clubs = "Yellow"
		hearts = "Green"
		ace = "1"
		ten = "2"
		king = "3"
		queen = "4"
		jack = "5"
	} else {
		spades = "Spades"
		diamonds = "Diamonds"
		clubs = "Clubs"
		hearts = "Hearts"
		ace = "A"
		ten = "10"
		king = "K"
		queen = "Q"
		jack = "J"
	}

	suits := []string{spades, diamonds, clubs, hearts}

	cardsMap := make(map[string][]Card)
	for _, c := range hand {
		key := c.Suit + "-" + c.Rank
		cardsMap[key] = append(cardsMap[key], c)
	}

	var meldCards []Card

	popCard := func(suit, rank string) (Card, bool) {
		key := suit + "-" + rank
		list := cardsMap[key]
		if len(list) > 0 {
			c := list[len(list)-1]
			cardsMap[key] = list[:len(list)-1]
			return c, true
		}
		return Card{}, false
	}

	cnt := func(suit, rank string) int {
		return len(cardsMap[suit+"-"+rank])
	}

	// 1. Trump Run
	rA := cnt(trumpSuit, ace)
	r10 := cnt(trumpSuit, ten)
	rK := cnt(trumpSuit, king)
	rQ := cnt(trumpSuit, queen)
	rJ := cnt(trumpSuit, jack)
	runs := min5(rA, r10, rK, rQ, rJ)
	for i := 0; i < runs; i++ {
		if c, ok := popCard(trumpSuit, ace); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(trumpSuit, ten); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(trumpSuit, king); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(trumpSuit, queen); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(trumpSuit, jack); ok { meldCards = append(meldCards, c) }
	}

	// 2. Royal Marriage (K, Q of trump)
	rK = cnt(trumpSuit, king)
	rQ = cnt(trumpSuit, queen)
	royalMarriages := min2(rK, rQ)
	for i := 0; i < royalMarriages; i++ {
		if c, ok := popCard(trumpSuit, king); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(trumpSuit, queen); ok { meldCards = append(meldCards, c) }
	}

	// 3. Common Marriages
	for _, s := range suits {
		if s == trumpSuit {
			continue
		}
		sK := cnt(s, king)
		sQ := cnt(s, queen)
		cm := min2(sK, sQ)
		for i := 0; i < cm; i++ {
			if c, ok := popCard(s, king); ok { meldCards = append(meldCards, c) }
			if c, ok := popCard(s, queen); ok { meldCards = append(meldCards, c) }
		}
	}

	// 4. Pinochles (Q of spades, J of diamonds)
	qSpades := cnt(spades, queen)
	jDiamonds := cnt(diamonds, jack)
	pinochles := min2(qSpades, jDiamonds)
	for i := 0; i < pinochles; i++ {
		if c, ok := popCard(spades, queen); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(diamonds, jack); ok { meldCards = append(meldCards, c) }
	}

	// 5. Aces Around
	aSpades := cnt(spades, ace)
	aDiamonds := cnt(diamonds, ace)
	aClubs := cnt(clubs, ace)
	aHearts := cnt(hearts, ace)
	aces := min4(aSpades, aDiamonds, aClubs, aHearts)
	for i := 0; i < aces; i++ {
		if c, ok := popCard(spades, ace); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(diamonds, ace); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(clubs, ace); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(hearts, ace); ok { meldCards = append(meldCards, c) }
	}

	// 6. Kings Around
	kSpades := cnt(spades, king)
	kDiamonds := cnt(diamonds, king)
	kClubs := cnt(clubs, king)
	kHearts := cnt(hearts, king)
	kings := min4(kSpades, kDiamonds, kClubs, kHearts)
	for i := 0; i < kings; i++ {
		if c, ok := popCard(spades, king); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(diamonds, king); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(clubs, king); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(hearts, king); ok { meldCards = append(meldCards, c) }
	}

	// 7. Queens Around
	qSpades = cnt(spades, queen)
	qDiamonds := cnt(diamonds, queen)
	qClubs := cnt(clubs, queen)
	qHearts := cnt(hearts, queen)
	queens := min4(qSpades, qDiamonds, qClubs, qHearts)
	for i := 0; i < queens; i++ {
		if c, ok := popCard(spades, queen); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(diamonds, queen); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(clubs, queen); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(hearts, queen); ok { meldCards = append(meldCards, c) }
	}

	// 8. Jacks Around
	jSpades := cnt(spades, jack)
	jDiamonds = cnt(diamonds, jack)
	jClubs := cnt(clubs, jack)
	jHearts := cnt(hearts, jack)
	jacks := min4(jSpades, jDiamonds, jClubs, jHearts)
	for i := 0; i < jacks; i++ {
		if c, ok := popCard(spades, jack); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(diamonds, jack); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(clubs, jack); ok { meldCards = append(meldCards, c) }
		if c, ok := popCard(hearts, jack); ok { meldCards = append(meldCards, c) }
	}

	return meldCards
}

func handleAndroidVersionCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{
		"version":     LatestAndroidVersion,
		"downloadUrl": "https://pinochle.bedrock.games/android/app-debug.apk",
	})
}

func handleAndroidApkDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", "attachment; filename=\"6pinochle-update.apk\"")
	http.ServeFile(w, r, "android/app/build/outputs/apk/debug/app-debug.apk")
}

