package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/syndtr/goleveldb/leveldb"
)

// ==================== TYPES ====================

type Block struct {
	Hash         string `json:"hash"`
	Height       int64  `json:"height"`
	Timestamp    int64  `json:"timestamp"`
	PrevHash     string `json:"prev_hash"`
	Nonce        uint32 `json:"nonce"`
	Miner        string `json:"miner"`
	Transactions []*Transaction `json:"transactions"`
}

type Transaction struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int64  `json:"amount"`
	Time   int64  `json:"time"`
}

type Wallet struct {
	Address string `json:"address"`
	Balance int64  `json:"balance"`
}

// ==================== PoP MINING ====================

type PoPSession struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	StartTime     int64     `json:"start_time"`
	EndTime       int64     `json:"end_time"`
	IsActive      bool      `json:"is_active"`
	ActivityScore float64   `json:"activity_score"`
	Reward        int64     `json:"reward"`
	Checks        []Check   `json:"checks"`
}

type Check struct {
	Time   int64   `json:"time"`
	Type   string  `json:"type"`
	Passed bool    `json:"passed"`
	Score  float64 `json:"score"`
}

var (
	activeSessions = make(map[string]*PoPSession)
	sessionsMu     sync.RWMutex
)

// Get session status
func GetSession(address string) *PoPSession {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	return activeSessions[address]
}

// Start 24h mining session
func StartMiningSession(address string) *PoPSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	
	// Check if already active
	if session, exists := activeSessions[address]; exists && session.IsActive {
		return session
	}
	
	now := time.Now().Unix()
	
	// SAFE: handle short addresses
	idSuffix := address
	if len(address) > 8 {
		idSuffix = address[:8]
	}
	
	session := &PoPSession{
		ID:            fmt.Sprintf("sess_%d_%s", now, idSuffix),
		Address:       address,
		StartTime:     now,
		EndTime:       now + (24 * 3600),
		IsActive:      true,
		ActivityScore: 1.0,
		Reward:        0,
		Checks:        []Check{},
	}
	
	activeSessions[address] = session
	
	// SAFE: handle short addresses for display
	displayAddr := address
	if len(address) > 20 {
		displayAddr = address[:20]
	}
	
	fmt.Printf("⛏️  Mining Started!\n")
	fmt.Printf("   User: %s...\n", displayAddr)
	fmt.Printf("   Start: %s\n", time.Unix(now, 0).Format("15:04:05"))
	fmt.Printf("   End: %s (24 hours)\n", time.Unix(session.EndTime, 0).Format("15:04:05"))
	
	return session
}

// Perform check (every 4 hours)
func PerformCheck(address string, checkType string) bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	
	session, exists := activeSessions[address]
	if !exists || !session.IsActive {
		return false
	}
	
	passed := true
	score := 1.0
	
	switch checkType {
	case "kyc":
		score = 1.0
	case "bot":
		score = 0.95 + (float64(time.Now().Unix()%10) / 100.0)
	case "activity":
		score = 1.0
	case "device":
		score = 1.0
	}
	
	check := Check{
		Time:   time.Now().Unix(),
		Type:   checkType,
		Passed: passed,
		Score:  score,
	}
	
	session.Checks = append(session.Checks, check)
	
	var totalScore float64
	for _, c := range session.Checks {
		totalScore += c.Score
	}
	session.ActivityScore = totalScore / float64(len(session.Checks))
	
	fmt.Printf("✅ Check: %s | Score: %.2f | Passed: %v\n", checkType, score, passed)
	
	return passed
}

// Complete session and calculate reward
func CompleteSession(address string, baseRate int64) *PoPSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	
	session, exists := activeSessions[address]
	if !exists || !session.IsActive {
		return nil
	}
	
	multiplier := 1.0
	if session.ActivityScore > 0.9 {
		multiplier = 1.0
	} else if session.ActivityScore > 0.7 {
		multiplier = 0.8
	} else if session.ActivityScore > 0.5 {
		multiplier = 0.5
	} else {
		multiplier = 0.0
	}
	
	session.Reward = int64(float64(baseRate) * session.ActivityScore * multiplier)
	session.IsActive = false
	
	fmt.Printf("\n✅ Mining Session Complete!\n")
	fmt.Printf("   Activity Score: %.2f\n", session.ActivityScore)
	fmt.Printf("   Base Rate: %d BORA\n", baseRate)
	fmt.Printf("   💰 Reward: %d BORA\n", session.Reward)
	
	return session
}

// ==================== DATABASE ====================

type DB struct {
	db *leveldb.DB
}

func NewDB() *DB {
	if _, err := os.Stat("./data"); os.IsNotExist(err) {
		os.MkdirAll("./data", 0755)
	}
	
	ldb, err := leveldb.OpenFile("./data/blockchain", nil)
	if err != nil {
		log.Fatal(err)
	}
	return &DB{db: ldb}
}

func (d *DB) SaveBlock(b *Block) {
	key := "block_" + b.Hash
	val, _ := json.Marshal(b)
	d.db.Put([]byte(key), val, nil)
	
	hKey := fmt.Sprintf("height_%d", b.Height)
	d.db.Put([]byte(hKey), []byte(b.Hash), nil)
	d.db.Put([]byte("tip"), []byte(b.Hash), nil)
}

func (d *DB) GetBlock(hash string) *Block {
	data, err := d.db.Get([]byte("block_"+hash), nil)
	if err != nil {
		return nil
	}
	var b Block
	json.Unmarshal(data, &b)
	return &b
}

func (d *DB) GetTip() string {
	data, _ := d.db.Get([]byte("tip"), nil)
	return string(data)
}

func (d *DB) GetBlockByHeight(h int64) *Block {
	data, _ := d.db.Get([]byte(fmt.Sprintf("height_%d", h)), nil)
	if data == nil {
		return nil
	}
	return d.GetBlock(string(data))
}

// ==================== BLOCKCHAIN ====================

type Blockchain struct {
	db      *DB
	mu      sync.RWMutex
	tip     string
	height  int64
	reward  int64
	mempool []*Transaction
}

func NewBlockchain() *Blockchain {
	db := NewDB()
	bc := &Blockchain{
		db:      db,
		reward:  50,
		mempool: make([]*Transaction, 0),
	}
	
	tip := db.GetTip()
	if tip == "" {
		bc.createGenesis()
	} else {
		bc.tip = tip
		bc.height = bc.getHeightFromDB()
	}
	
	return bc
}

func (bc *Blockchain) createGenesis() {
	genesis := &Block{
		Hash:      bc.calculateHash(0, "0", 0, "Genesis"),
		Height:    0,
		Timestamp: time.Now().Unix(),
		PrevHash:  "0",
		Nonce:     0,
		Miner:     "System",
		Transactions: []*Transaction{
			{ID: "genesis", From: "0", To: "0", Amount: 0, Time: time.Now().Unix()},
		},
	}
	
	bc.db.SaveBlock(genesis)
	bc.tip = genesis.Hash
	bc.height = 0
	fmt.Println("⛓️  Genesis created:", genesis.Hash[:16])
}

func (bc *Blockchain) calculateHash(height int64, prev string, nonce uint32, data string) string {
	record := fmt.Sprintf("%d%s%d%s", height, prev, nonce, data)
	hash := sha256.Sum256([]byte(record))
	return hex.EncodeToString(hash[:])
}

func (bc *Blockchain) Mine(miner string) *Block {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	
	target := big.NewInt(1)
	target.Lsh(target, uint(256-16))
	
	var nonce uint32
	var hash string
	var hashInt big.Int
	
	txs := bc.mempool
	if len(txs) > 999 {
		txs = txs[:999]
	}
	
	coinbase := &Transaction{
		ID:     fmt.Sprintf("coinbase_%d", bc.height+1),
		From:   "mining",
		To:     miner,
		Amount: bc.reward,
		Time:   time.Now().Unix(),
	}
	allTxs := append([]*Transaction{coinbase}, txs...)
	
	fmt.Printf("⛏️  Mining block #%d...\n", bc.height+1)
	
	for nonce < ^uint32(0) {
		hash = bc.calculateHash(bc.height+1, bc.tip, nonce, bc.serializeTxs(allTxs))
		hashInt.SetString(hash, 16)
		
		if hashInt.Cmp(target) == -1 {
			fmt.Printf("   ✅ Mined! Nonce: %d | Hash: %s...\n", nonce, hash[:16])
			break
		}
		nonce++
	}
	
	block := &Block{
		Hash:         hash,
		Height:       bc.height + 1,
		Timestamp:    time.Now().Unix(),
		PrevHash:     bc.tip,
		Nonce:        nonce,
		Miner:        miner,
		Transactions: allTxs,
	}
	
	bc.db.SaveBlock(block)
	bc.tip = block.Hash
	bc.height++
	bc.mempool = bc.mempool[len(txs):]
	
	return block
}

func (bc *Blockchain) serializeTxs(txs []*Transaction) string {
	data, _ := json.Marshal(txs)
	return string(data)
}

func (bc *Blockchain) GetBalance(addr string) int64 {
	return 0
}

func (bc *Blockchain) AddTx(from, to string, amount int64) *Transaction {
	tx := &Transaction{
		ID:     fmt.Sprintf("tx_%d", time.Now().UnixNano()),
		From:   from,
		To:     to,
		Amount: amount,
		Time:   time.Now().Unix(),
	}
	
	bc.mu.Lock()
	bc.mempool = append(bc.mempool, tx)
	bc.mu.Unlock()
	
	return tx
}

func (bc *Blockchain) GetLatest() *Block {
	return bc.db.GetBlock(bc.tip)
}

func (bc *Blockchain) GetHeight() int64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.height
}

func (bc *Blockchain) getHeightFromDB() int64 {
	return 0
}

// ==================== API ====================

var bc *Blockchain

func main() {
	fmt.Println("🚀 Blockora Blockchain v3.0")
	
	bc = NewBlockchain()
	
	fmt.Printf("📦 Height: %d | Reward: %d BORA\n", bc.GetHeight(), bc.reward)
	
	r := mux.NewRouter()
	
	r.HandleFunc("/api/status", handleStatus).Methods("GET")
	r.HandleFunc("/api/wallet/create", handleCreateWallet).Methods("POST")
	r.HandleFunc("/api/mining/block", handleMine).Methods("POST")
	r.HandleFunc("/api/transaction", handleTx).Methods("POST")
	r.HandleFunc("/api/block/{height}", handleGetBlock).Methods("GET")
	r.HandleFunc("/api/pop/start", handlePoPStart).Methods("POST")
	r.HandleFunc("/api/pop/check", handlePoPCheck).Methods("POST")
	r.HandleFunc("/api/pop/complete", handlePoPComplete).Methods("POST")
	r.HandleFunc("/api/pop/status/{address}", handlePoPStatus).Methods("GET")
	
	handler := enableCORS(r)
	
	fmt.Println("🌐 Server: http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", handler))
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	block := bc.GetLatest()
	respond(w, map[string]interface{}{
		"success": true,
		"height":  bc.GetHeight(),
		"tip":     block.Hash,
		"mempool": len(bc.mempool),
	})
}

func handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	addr := "Bx" + hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[:40]
	respond(w, map[string]interface{}{
		"success": true,
		"address": addr,
	})
}

func handleMine(w http.ResponseWriter, r *http.Request) {
	var req struct{ Miner string `json:"miner"` }
	json.NewDecoder(r.Body).Decode(&req)
	
	if req.Miner == "" {
		req.Miner = "unknown"
	}
	
	block := bc.Mine(req.Miner)
	
	respond(w, map[string]interface{}{
		"success":    true,
		"block_hash": block.Hash,
		"height":     block.Height,
		"nonce":      block.Nonce,
		"miner":      block.Miner,
		"tx_count":   len(block.Transactions),
	})
}

func handleTx(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	
	tx := bc.AddTx(req.From, req.To, req.Amount)
	
	respond(w, map[string]interface{}{
		"success": true,
		"tx_id":   tx.ID,
	})
}

func handleGetBlock(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	h, _ := strconv.ParseInt(vars["height"], 10, 64)
	block := bc.db.GetBlockByHeight(h)
	
	if block == nil {
		respond(w, map[string]interface{}{"success": false, "error": "not found"})
		return
	}
	
	respond(w, map[string]interface{}{
		"success": true,
		"block":   block,
	})
}

func respond(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func enableCORS(next http.Handler) http.Handler {
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

// ==================== PoP HANDLERS ====================

func handlePoPStart(w http.ResponseWriter, r *http.Request) {
	var req struct{ Address string `json:"address"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, map[string]interface{}{"success": false, "error": "invalid request"})
		return
	}
	
	if req.Address == "" {
		respond(w, map[string]interface{}{"success": false, "error": "address required"})
		return
	}
	
	session := StartMiningSession(req.Address)
	
	respond(w, map[string]interface{}{
		"success": true,
		"session": session,
	})
}

func handlePoPCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		Type    string `json:"type"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	
	passed := PerformCheck(req.Address, req.Type)
	
	respond(w, map[string]interface{}{
		"success": passed,
		"check":   req.Type,
	})
}

func handlePoPComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address  string `json:"address"`
		BaseRate int64  `json:"base_rate"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	
	session := CompleteSession(req.Address, req.BaseRate)
	if session == nil {
		respond(w, map[string]interface{}{
			"success": false,
			"error":   "Session not found",
		})
		return
	}
	
	// Mine block with reward
	bc.Mine(req.Address)
	
	respond(w, map[string]interface{}{
		"success": true,
		"reward":  session.Reward,
	})
}

func handlePoPStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	
	session := GetSession(address)
	if session == nil {
		respond(w, map[string]interface{}{
			"success": false,
			"error":   "No active session",
		})
		return
	}
	
	remaining := session.EndTime - time.Now().Unix()
	if remaining < 0 {
		remaining = 0
	}
	
	respond(w, map[string]interface{}{
		"success":        true,
		"active":         session.IsActive,
		"activity_score": session.ActivityScore,
		"remaining_time": remaining,
	})
}
