package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// =============================================================================
// Event Types & Topic Mapping
// =============================================================================

const (
	// Trading events
	EventTradeExecuted = "TRADE_EXECUTED"
	EventBuyCompleted  = "BUY_COMPLETED"
	EventSellCompleted = "SELL_COMPLETED"

	// Liquidity events
	EventLiquidityAdded      = "LIQUIDITY_ADDED"
	EventLiquidityRemoved    = "LIQUIDITY_REMOVED"
	EventLiquidityRebalanced = "LIQUIDITY_REBALANCED"

	// Metadata events
	EventMetadataUpdated   = "METADATA_UPDATED"
	EventTokenNameChanged  = "TOKEN_NAME_CHANGED"
	EventTokenFlagsChanged = "TOKEN_FLAGS_CHANGED"

	// Social events
	EventTwitterVerified    = "TWITTER_VERIFIED"
	EventSocialLinksUpdated = "SOCIAL_LINKS_UPDATED"

	// Community events
	EventCommentPosted   = "COMMENT_POSTED"
	EventCommentPinned   = "COMMENT_PINNED"
	EventCommentUpvoted  = "COMMENT_UPVOTED"
	EventFavoriteToggled = "FAVORITE_TOGGLED"

	// Creation events
	EventTokenCreated = "TOKEN_CREATED"
	EventTokenListed  = "TOKEN_LISTED"

	// Analytics events
	EventPriceDeltaUpdated     = "PRICE_DELTA_UPDATED"
	EventHolderCountUpdated    = "HOLDER_COUNT_UPDATED"
	EventAnalyticsRecalculated = "ANALYTICS_RECALCULATED"
	EventTrendingUpdated       = "TRENDING_UPDATED"

	// Balance events
	EventBalanceUpdated    = "BALANCE_UPDATED"
	EventTransferCompleted = "TRANSFER_COMPLETED"
)

// Topic names
const (
	TopicTrades    = "odin.trades"
	TopicLiquidity = "odin.liquidity"
	TopicMetadata  = "odin.metadata"
	TopicSocial    = "odin.social"
	TopicCommunity = "odin.community"
	TopicCreation  = "odin.creation"
	TopicAnalytics = "odin.analytics"
	TopicBalances  = "odin.balances"
)

// EventTypeTopics maps event types to their Kafka topics
var EventTypeTopics = map[string]string{
	// Trading events → odin.trades
	EventTradeExecuted: TopicTrades,
	EventBuyCompleted:  TopicTrades,
	EventSellCompleted: TopicTrades,

	// Liquidity events → odin.liquidity
	EventLiquidityAdded:      TopicLiquidity,
	EventLiquidityRemoved:    TopicLiquidity,
	EventLiquidityRebalanced: TopicLiquidity,

	// Metadata events → odin.metadata
	EventMetadataUpdated:   TopicMetadata,
	EventTokenNameChanged:  TopicMetadata,
	EventTokenFlagsChanged: TopicMetadata,

	// Social events → odin.social
	EventTwitterVerified:    TopicSocial,
	EventSocialLinksUpdated: TopicSocial,

	// Community events → odin.community
	EventCommentPosted:   TopicCommunity,
	EventCommentPinned:   TopicCommunity,
	EventCommentUpvoted:  TopicCommunity,
	EventFavoriteToggled: TopicCommunity,

	// Creation events → odin.creation
	EventTokenCreated: TopicCreation,
	EventTokenListed:  TopicCreation,

	// Analytics events → odin.analytics
	EventPriceDeltaUpdated:     TopicAnalytics,
	EventHolderCountUpdated:    TopicAnalytics,
	EventAnalyticsRecalculated: TopicAnalytics,
	EventTrendingUpdated:       TopicAnalytics,

	// Balance events → odin.balances
	EventBalanceUpdated:    TopicBalances,
	EventTransferCompleted: TopicBalances,
}

// Weighted event distribution
type weightedEvent struct {
	eventType string
	weight    float64
}

var eventWeights = []weightedEvent{
	// High frequency events (50%)
	{EventTradeExecuted, 20},
	{EventBuyCompleted, 15},
	{EventSellCompleted, 15},

	// Medium frequency events (30%)
	{EventLiquidityAdded, 8},
	{EventLiquidityRemoved, 8},
	{EventPriceDeltaUpdated, 7},
	{EventAnalyticsRecalculated, 7},

	// Low frequency events (20%)
	{EventMetadataUpdated, 5},
	{EventCommentPosted, 5},
	{EventBalanceUpdated, 3},
	{EventHolderCountUpdated, 2},
	{EventFavoriteToggled, 2},
	{EventCommentUpvoted, 1},
	{EventTrendingUpdated, 1},
	{EventSocialLinksUpdated, 1},

	// Rare events (<1%)
	{EventTokenCreated, 0.5},
	{EventTokenListed, 0.5},
	{EventTokenNameChanged, 0.3},
	{EventTokenFlagsChanged, 0.3},
	{EventTwitterVerified, 0.2},
	{EventCommentPinned, 0.2},
	{EventLiquidityRebalanced, 0.1},
	{EventTransferCompleted, 0.1},
}

var totalWeight float64

func init() {
	for _, w := range eventWeights {
		totalWeight += w.weight
	}
}

// =============================================================================
// Types
// =============================================================================

// Config holds application configuration
type Config struct {
	KafkaBrokers []string
	APIPort      int
}

// Event represents a token event
type Event struct {
	Type      string                 `json:"type"`
	TokenID   string                 `json:"tokenId"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// KafkaMessage is the message format sent to Kafka
type KafkaMessage struct {
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// =============================================================================
// Config Loading
// =============================================================================

func loadConfig() *Config {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "localhost:19092"
	}

	portStr := os.Getenv("API_PORT")
	port := 3001
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	return &Config{
		KafkaBrokers: strings.Split(brokers, ","),
		APIPort:      port,
	}
}

// =============================================================================
// Random Data Generators
// =============================================================================

func randomPrice() float64 {
	return rand.Float64() * 100
}

func randomAmount() float64 {
	return rand.Float64() * 1000
}

func randomAddress() string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, 40)
	for i := range result {
		result[i] = hexChars[rand.Intn(len(hexChars))]
	}
	return "0x" + string(result)
}

func randomHash() string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, 64)
	for i := range result {
		result[i] = hexChars[rand.Intn(len(hexChars))]
	}
	return "0x" + string(result)
}

func randomSymbol() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	length := 3 + rand.Intn(3)
	result := make([]byte, length)
	for i := range result {
		result[i] = letters[rand.Intn(len(letters))]
	}
	return string(result)
}

func randomBool() bool {
	return rand.Float64() > 0.5
}

// =============================================================================
// Event Data Generators
// =============================================================================

func generateEventData(eventType, tokenID string) map[string]interface{} {
	switch eventType {
	case EventTradeExecuted, EventBuyCompleted, EventSellCompleted:
		return map[string]interface{}{
			"price":  randomPrice(),
			"amount": randomAmount(),
			"buyer":  randomAddress(),
			"seller": randomAddress(),
			"txHash": randomHash(),
		}

	case EventLiquidityAdded, EventLiquidityRemoved:
		return map[string]interface{}{
			"amount":         randomAmount(),
			"provider":       randomAddress(),
			"totalLiquidity": randomAmount() * 10,
		}

	case EventLiquidityRebalanced:
		return map[string]interface{}{
			"oldRatio":       rand.Float64(),
			"newRatio":       rand.Float64(),
			"totalLiquidity": randomAmount() * 10,
		}

	case EventMetadataUpdated, EventTokenNameChanged:
		return map[string]interface{}{
			"name":        fmt.Sprintf("Token %s", tokenID),
			"symbol":      randomSymbol(),
			"description": "Updated token metadata",
		}

	case EventTokenFlagsChanged:
		return map[string]interface{}{
			"flags": map[string]interface{}{
				"verified": rand.Float64() > 0.5,
				"featured": rand.Float64() > 0.8,
				"trending": rand.Float64() > 0.7,
			},
		}

	case EventTwitterVerified:
		return map[string]interface{}{
			"twitterHandle": fmt.Sprintf("@token%s", tokenID),
			"verified":      true,
			"verifiedAt":    time.Now().UnixMilli(),
		}

	case EventSocialLinksUpdated:
		return map[string]interface{}{
			"twitter":  fmt.Sprintf("https://twitter.com/token%s", tokenID),
			"telegram": fmt.Sprintf("https://t.me/token%s", tokenID),
			"website":  fmt.Sprintf("https://token%s.com", tokenID),
		}

	case EventCommentPosted:
		return map[string]interface{}{
			"commentId": randomHash(),
			"author":    randomAddress(),
			"content":   "This is a test comment",
			"timestamp": time.Now().UnixMilli(),
		}

	case EventCommentPinned:
		return map[string]interface{}{
			"commentId": randomHash(),
			"pinnedBy":  randomAddress(),
		}

	case EventCommentUpvoted:
		return map[string]interface{}{
			"commentId":    randomHash(),
			"voter":        randomAddress(),
			"totalUpvotes": rand.Intn(100),
		}

	case EventFavoriteToggled:
		return map[string]interface{}{
			"userId":     randomAddress(),
			"isFavorite": randomBool(),
		}

	case EventTokenCreated:
		return map[string]interface{}{
			"creator": randomAddress(),
			"name":    fmt.Sprintf("Token %s", tokenID),
			"symbol":  randomSymbol(),
			"supply":  randomAmount() * 1000000,
		}

	case EventTokenListed:
		return map[string]interface{}{
			"exchange":     "Odin DEX",
			"initialPrice": randomPrice(),
			"timestamp":    time.Now().UnixMilli(),
		}

	case EventPriceDeltaUpdated:
		return map[string]interface{}{
			"price":    randomPrice(),
			"delta1h":  (rand.Float64() - 0.5) * 20,
			"delta24h": (rand.Float64() - 0.5) * 50,
			"delta7d":  (rand.Float64() - 0.5) * 100,
		}

	case EventHolderCountUpdated:
		return map[string]interface{}{
			"holderCount": rand.Intn(10000),
			"change":      int((rand.Float64() - 0.5) * 100),
		}

	case EventAnalyticsRecalculated:
		return map[string]interface{}{
			"volume24h": randomAmount() * 10000,
			"marketCap": randomAmount() * 1000000,
			"liquidity": randomAmount() * 100000,
		}

	case EventTrendingUpdated:
		return map[string]interface{}{
			"rank":  rand.Intn(100) + 1,
			"score": rand.Float64() * 1000,
		}

	case EventBalanceUpdated:
		return map[string]interface{}{
			"user":    randomAddress(),
			"balance": randomAmount() * 1000,
			"change":  (rand.Float64() - 0.5) * 100,
		}

	case EventTransferCompleted:
		return map[string]interface{}{
			"from":   randomAddress(),
			"to":     randomAddress(),
			"amount": randomAmount(),
			"txHash": randomHash(),
		}

	default:
		return map[string]interface{}{}
	}
}

// =============================================================================
// Weighted Event Selection
// =============================================================================

func selectRandomEventType() string {
	r := rand.Float64() * totalWeight
	for _, w := range eventWeights {
		r -= w.weight
		if r <= 0 {
			return w.eventType
		}
	}
	return EventTradeExecuted
}

func generateRandomEvent(tokenIDs []string) Event {
	tokenID := tokenIDs[rand.Intn(len(tokenIDs))]
	eventType := selectRandomEventType()
	timestamp := time.Now().UnixMilli()

	return Event{
		Type:      eventType,
		TokenID:   tokenID,
		Timestamp: timestamp,
		Data:      generateEventData(eventType, tokenID),
	}
}

// =============================================================================
// Kafka Publisher
// =============================================================================

type Publisher struct {
	client       *kgo.Client
	isConnected  bool
	publishCount int64
	lastLogTime  time.Time
	lastCount    int64
	statsStopCh  chan struct{}
	mu           sync.RWMutex
}

func NewPublisher(brokers []string) (*Publisher, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("odin-publisher"),
		kgo.ProducerBatchMaxBytes(1024 * 1024), // 1MB
		kgo.MaxBufferedRecords(10000),
		kgo.RecordRetries(8),
		kgo.RetryBackoffFn(func(attempt int) time.Duration {
			return time.Duration(100*(1<<attempt)) * time.Millisecond
		}),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	return &Publisher{
		client:      client,
		statsStopCh: make(chan struct{}),
	}, nil
}

func (p *Publisher) Connect(ctx context.Context) error {
	// Ping to verify connection
	if err := p.client.Ping(ctx); err != nil {
		return fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	p.mu.Lock()
	p.isConnected = true
	p.lastLogTime = time.Now()
	p.lastCount = 0
	p.mu.Unlock()

	log.Println("[Publisher] Connected to Kafka")

	// Start stats logging goroutine
	go p.statsLogger()

	return nil
}

func (p *Publisher) statsLogger() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.RLock()
			now := time.Now()
			elapsed := now.Sub(p.lastLogTime).Seconds()
			count := atomic.LoadInt64(&p.publishCount)
			eventsPublished := count - p.lastCount
			p.mu.RUnlock()

			if eventsPublished > 0 {
				rate := float64(eventsPublished) / elapsed
				log.Printf("[Publisher] Published %d events in last %.1fs (%.1f events/sec) | Total: %d",
					eventsPublished, elapsed, rate, count)
			}

			p.mu.Lock()
			p.lastLogTime = now
			p.lastCount = count
			p.mu.Unlock()

		case <-p.statsStopCh:
			return
		}
	}
}

func (p *Publisher) Publish(ctx context.Context, event Event) error {
	p.mu.RLock()
	if !p.isConnected {
		p.mu.RUnlock()
		return fmt.Errorf("publisher not connected")
	}
	p.mu.RUnlock()

	topic, ok := EventTypeTopics[event.Type]
	if !ok {
		return fmt.Errorf("unknown event type: %s", event.Type)
	}

	// Create message payload
	msg := KafkaMessage{
		Type:      event.Type,
		Timestamp: event.Timestamp,
		Data:      event.Data,
	}

	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	record := &kgo.Record{
		Topic:     topic,
		Key:       []byte(event.TokenID),
		Value:     value,
		Timestamp: time.UnixMilli(event.Timestamp),
	}

	// Produce synchronously
	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	atomic.AddInt64(&p.publishCount, 1)

	log.Printf("[Publisher] Published %s for token %s to %s", event.Type, event.TokenID, topic)

	return nil
}

func (p *Publisher) Close() {
	close(p.statsStopCh)

	p.mu.Lock()
	p.isConnected = false
	p.mu.Unlock()

	p.client.Close()

	count := atomic.LoadInt64(&p.publishCount)
	log.Printf("[Publisher] Disconnected after publishing %d total events", count)
}

func (p *Publisher) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isConnected
}

func (p *Publisher) GetPublishCount() int64 {
	return atomic.LoadInt64(&p.publishCount)
}

// =============================================================================
// Event Simulator
// =============================================================================

type Simulator struct {
	publisher   *Publisher
	tokenIDs    []string
	rate        int
	isRunning   bool
	stopCh      chan struct{}
	eventCount  int64
	lastLogTime time.Time
	lastCount   int64
	statsStopCh chan struct{}
	mu          sync.RWMutex
}

func NewSimulator(publisher *Publisher) *Simulator {
	return &Simulator{
		publisher: publisher,
	}
}

func (s *Simulator) Start(ctx context.Context, rate int, tokenIDs []string) {
	s.mu.Lock()
	if s.isRunning {
		log.Println("[Simulator] Already running")
		s.mu.Unlock()
		return
	}

	s.tokenIDs = tokenIDs
	s.rate = rate
	s.isRunning = true
	s.stopCh = make(chan struct{})
	s.statsStopCh = make(chan struct{})
	s.eventCount = 0
	s.lastLogTime = time.Now()
	s.lastCount = 0
	s.mu.Unlock()

	log.Printf("[Simulator] Starting at %d events/sec", rate)

	// Start event generation goroutine
	go s.generateEvents(ctx)

	// Start stats logging goroutine
	go s.statsLogger()
}

func (s *Simulator) generateEvents(ctx context.Context) {
	interval := time.Second / time.Duration(s.rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.mu.RLock()
			tokenIDs := s.tokenIDs
			s.mu.RUnlock()

			event := generateRandomEvent(tokenIDs)
			atomic.AddInt64(&s.eventCount, 1)

			log.Printf("[Simulator] Generated %s for token %s", event.Type, event.TokenID)

			if err := s.publisher.Publish(ctx, event); err != nil {
				log.Printf("[Simulator] Failed to publish event: %v", err)
			}

		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Simulator) statsLogger() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.mu.RLock()
			now := time.Now()
			elapsed := now.Sub(s.lastLogTime).Seconds()
			count := atomic.LoadInt64(&s.eventCount)
			eventsGenerated := count - s.lastCount
			s.mu.RUnlock()

			rate := float64(eventsGenerated) / elapsed
			log.Printf("[Simulator] Generated %d events in last %.1fs (%.1f events/sec) | Total: %d",
				eventsGenerated, elapsed, rate, count)

			s.mu.Lock()
			s.lastLogTime = now
			s.lastCount = count
			s.mu.Unlock()

		case <-s.statsStopCh:
			return
		}
	}
}

func (s *Simulator) Stop() {
	s.mu.Lock()
	if !s.isRunning {
		s.mu.Unlock()
		return
	}

	close(s.stopCh)
	close(s.statsStopCh)
	s.isRunning = false
	count := atomic.LoadInt64(&s.eventCount)
	s.mu.Unlock()

	log.Printf("[Simulator] Stopped after generating %d total events", count)
}

func (s *Simulator) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

func (s *Simulator) GetRate() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rate
}

func (s *Simulator) GetTokenIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokenIDs
}

// =============================================================================
// HTTP API Server
// =============================================================================

type Server struct {
	publisher *Publisher
	simulator *Simulator
	server    *http.Server
}

func NewServer(publisher *Publisher, port int) *Server {
	s := &Server{
		publisher: publisher,
		simulator: NewSimulator(publisher),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/rate", s.handleRate)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: corsMiddleware(mux),
	}

	return s
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) Start() error {
	log.Printf("[Server] Listening on %s", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	simulatorStatus := "stopped"
	if s.simulator.IsRunning() {
		simulatorStatus = "running"
	}

	publisherStatus := "disconnected"
	if s.publisher.IsHealthy() {
		publisherStatus = "connected"
	}

	resp := map[string]interface{}{
		"status":      "ok",
		"publisher":   publisherStatus,
		"simulator":   simulatorStatus,
		"currentRate": s.simulator.GetRate(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]interface{}{
		"isRunning":        s.simulator.IsRunning(),
		"currentRate":      s.simulator.GetRate(),
		"publisherHealthy": s.publisher.IsHealthy(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type startRequest struct {
	Rate     int      `json:"rate"`
	TokenIDs []string `json:"tokenIds"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Rate <= 0 {
		req.Rate = 100 // Default rate
	}

	if len(req.TokenIDs) == 0 {
		resp := map[string]string{"error": "tokenIds array is required and must not be empty"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Stop existing simulator if running
	if s.simulator.IsRunning() {
		s.simulator.Stop()
	}

	// Create new simulator and start
	// Use background context so simulator keeps running after HTTP response is sent
	s.simulator = NewSimulator(s.publisher)
	s.simulator.Start(context.Background(), req.Rate, req.TokenIDs)

	resp := map[string]interface{}{
		"status":     "started",
		"rate":       req.Rate,
		"tokenCount": len(req.TokenIDs),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.simulator.Stop()

	resp := map[string]string{"status": "stopped"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type rateRequest struct {
	Rate int `json:"rate"`
}

func (s *Server) handleRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req rateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Rate <= 0 {
		resp := map[string]string{"error": "rate must be a positive number"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(resp)
		return
	}

	if !s.simulator.IsRunning() {
		resp := map[string]string{"error": "Simulator not running. Call /start first."}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Get current tokenIDs and restart with new rate
	tokenIDs := s.simulator.GetTokenIDs()
	s.simulator.Stop()

	// Use background context so simulator keeps running after HTTP response is sent
	s.simulator = NewSimulator(s.publisher)
	s.simulator.Start(context.Background(), req.Rate, tokenIDs)

	resp := map[string]interface{}{
		"status": "updated",
		"rate":   req.Rate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// =============================================================================
// Main
// =============================================================================

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	config := loadConfig()

	log.Printf("[Main] Starting publisher with brokers: %v", config.KafkaBrokers)

	// Create publisher
	publisher, err := NewPublisher(config.KafkaBrokers)
	if err != nil {
		log.Fatalf("[Main] Failed to create publisher: %v", err)
	}

	// Connect to Kafka
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := publisher.Connect(ctx); err != nil {
		cancel()
		log.Fatalf("[Main] Failed to connect to Kafka: %v", err)
	}
	cancel()

	// Create and start HTTP server
	server := NewServer(publisher, config.APIPort)

	// Start server in goroutine
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Main] Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("[Main] Shutting down...")

	// Graceful shutdown
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[Main] Server shutdown error: %v", err)
	}

	publisher.Close()

	log.Println("[Main] Shutdown complete")
}
