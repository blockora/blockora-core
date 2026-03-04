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
	
	// Save height index
	hKey := fmt.Sprintf("height_%d", b.Height)
	d.db.Put([]byte(hKey), []byte(b.Hash), nil)
	
	// Save tip
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
	db       *DB
	mu       sync.RWMutex
	tip      string
	height   int64
	reward   int64
	mempool  []*Transaction
}

func NewBlockchain() *Blockchain {
	db := NewDB()
	bc := &Blockchain{
		db:     db,
		reward: 50,
		mempool: make([]*Transaction, 0),
	}
	
	// Load or create genesis
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
	
	// Simple mining (TargetBits = 16 for fast testing)
	target := big.NewInt(1)
	target.Lsh(target, uint(256-16))
	
	var nonce uint32
	var hash string
	var hashInt big.Int
	
	txs := bc.mempool
	if len(txs) > 999 {
		txs = txs[:999]
	}
	
	// Coinbase
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
	// Simple: scan all blocks
	// In production: use UTXO set
	return 0 // Simplified for now
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
	// Simplified
	return 0
}

// ==================== API ====================

var bc *Blockchain

func main() {
	fmt.Println("🚀 Blockora Blockchain v3.0")
	
	bc = NewBlockchain()
	
	fmt.Printf("📦 Height: %d | Reward: %d BORA\n", bc.GetHeight(), bc.reward)
	
	r := mux.NewRouter()
	
	// Routes
	r.HandleFunc("/api/status", handleStatus).Methods("GET")
	r.HandleFunc("/api/wallet/create", handleCreateWallet).Methods("POST")
	r.HandleFunc("/api/mining/block", handleMine).Methods("POST")
	r.HandleFunc("/api/transaction", handleTx).Methods("POST")
	r.HandleFunc("/api/block/{height}", handleGetBlock).Methods("GET")
	
	// CORS
	handler := enableCORS(r)
	
	fmt.Println("🌐 Server: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
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
