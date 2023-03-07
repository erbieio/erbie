// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package miner

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/xerrors"

	"github.com/ethereum/go-ethereum/trie"

	"github.com/ethereum/go-ethereum/core/rawdb"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// resultQueueSize is the size of channel listening to sealing result.
	resultQueueSize = 10

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	// chainSideChanSize is the size of channel listening to ChainSideEvent.
	chainSideChanSize = 10

	// resubmitAdjustChanSize is the size of resubmitting interval adjustment channel.
	resubmitAdjustChanSize = 10

	// miningLogAtDepth is the number of confirmations before logging successful mining.
	miningLogAtDepth = 7

	// minRecommitInterval is the minimal time interval to recreate the mining block with
	// any newly arrived transactions.
	minRecommitInterval = 2 * time.Second

	// maxRecommitInterval is the maximum time interval to recreate the mining block with
	// any newly arrived transactions.
	maxRecommitInterval = 15 * time.Second

	// intervalAdjustRatio is the impact a single interval adjustment has on sealing work
	// resubmitting interval.
	intervalAdjustRatio = 0.1

	// intervalAdjustBias is applied during the new resubmit interval calculation in favor of
	// increasing upper limit or decreasing lower limit so that the limit can be reachable.
	intervalAdjustBias = 200 * 1000.0 * 1000.0

	// staleThreshold is the maximum depth of the acceptable stale block.
	staleThreshold = 7

	// Send online proof transactions every 1000 blocks
	activeCycle = 30
)

// environment is the worker's current environment and holds all of the current state information.
type environment struct {
	signer types.Signer

	state     *state.StateDB // apply state changes here
	ancestors mapset.Set     // ancestor set (used for checking uncle parent validity)
	family    mapset.Set     // family set (used for checking uncle invalidity)
	uncles    mapset.Set     // uncle set
	tcount    int            // tx count in cycle
	gasPool   *core.GasPool  // available gas used to pack transactions

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt
}

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	receipts  []*types.Receipt
	state     *state.StateDB
	block     *types.Block
	createdAt time.Time
}

const (
	commitInterruptNone int32 = iota
	commitInterruptNewHead
	commitInterruptResubmit
)

// newWorkReq represents a request for new sealing work submitting with relative interrupt notifier.
type newWorkReq struct {
	interrupt *int32
	noempty   bool
	timestamp int64
}

// intervalAdjust represents a resubmitting interval adjustment.
type intervalAdjust struct {
	ratio float64
	inc   bool
}

// worker is the main object which takes care of submitting new work to consensus engine
// and gathering the sealing result.
type worker struct {
	config      *Config
	chainConfig *params.ChainConfig
	engine      consensus.Engine
	eth         Backend
	chain       *core.BlockChain

	// Feeds
	pendingLogsFeed event.Feed

	// Subscriptions
	mux          *event.TypeMux
	txsCh        chan core.NewTxsEvent
	txsSub       event.Subscription
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription
	chainSideCh  chan core.ChainSideEvent
	chainSideSub event.Subscription

	// Channels
	newWorkCh chan *newWorkReq

	isEmpty            bool
	taskCh             chan *task
	resultCh           chan *types.Block
	startCh            chan struct{}
	onlineCh           chan struct{}
	emptyCh            chan struct{}
	exitCh             chan struct{}
	resubmitIntervalCh chan time.Duration
	resubmitAdjustCh   chan *intervalAdjust
	notifyBlockCh      chan *types.OnlineValidatorList

	current      *environment
	emptycurrent *environment
	proofcurrent *environment
	localUncles  map[common.Hash]*types.Block // A set of side blocks generated locally as the possible uncle blocks.
	remoteUncles map[common.Hash]*types.Block // A set of side blocks as the possible uncle blocks.
	unconfirmed  *unconfirmedBlocks           // A set of locally mined blocks pending canonicalness confirmations.

	mu       sync.RWMutex // The lock used to protect the coinbase and extra fields
	coinbase common.Address
	extra    []byte

	pendingMu    sync.RWMutex
	pendingTasks map[common.Hash]*task

	snapshotMu       sync.RWMutex // The lock used to protect the snapshots below
	snapshotBlock    *types.Block
	snapshotReceipts types.Receipts
	snapshotState    *state.StateDB

	// atomic status counters
	running int32 // The indicator whether the consensus engine is running or not.
	newTxs  int32 // New arrival transaction count since last sealing work submitting.

	// noempty is the flag used to control whether the feature of pre-seal empty
	// block is enabled. The default value is false(pre-seal is enabled by default).
	// But in some special scenario the consensus engine will seal blocks instantaneously,
	// in this case this feature will add all empty blocks into canonical chain
	// non-stop and no real transaction will be included.
	noempty uint32

	// External functions
	isLocalBlock func(block *types.Block) bool // Function used to determine whether the specified block is mined by local miner.

	// Test hooks
	newTaskHook  func(*task)                        // Method to call upon receiving a new sealing task.
	skipSealHook func(*task) bool                   // Method to decide whether skipping the sealing.
	fullTaskHook func()                             // Method to call before pushing the full sealing task.
	resubmitHook func(time.Duration, time.Duration) // Method to call upon updating resubmitting interval.
	cerytify     *Certify
	miner        Handler
	// onlineValidators
	onlineValidators *types.OnlineValidatorList

	//empty block
	totalCondition      int
	emptyTimestamp      int64
	emptyHandleFlag     bool
	cacheHeight         *big.Int
	targetWeightBalance *big.Int
	emptyTimer          *time.Timer
	resetEmptyCh        chan struct{}
}

func newWorker(handler Handler, config *Config, chainConfig *params.ChainConfig, engine consensus.Engine, eth Backend, mux *event.TypeMux, isLocalBlock func(*types.Block) bool, init bool) *worker {
	worker := &worker{
		config:              config,
		chainConfig:         chainConfig,
		engine:              engine,
		eth:                 eth,
		mux:                 mux,
		chain:               eth.BlockChain(),
		isLocalBlock:        isLocalBlock,
		localUncles:         make(map[common.Hash]*types.Block),
		remoteUncles:        make(map[common.Hash]*types.Block),
		unconfirmed:         newUnconfirmedBlocks(eth.BlockChain(), miningLogAtDepth),
		pendingTasks:        make(map[common.Hash]*task),
		txsCh:               make(chan core.NewTxsEvent, txChanSize),
		chainHeadCh:         make(chan core.ChainHeadEvent, chainHeadChanSize),
		chainSideCh:         make(chan core.ChainSideEvent, chainSideChanSize),
		newWorkCh:           make(chan *newWorkReq),
		taskCh:              make(chan *task),
		resultCh:            make(chan *types.Block, resultQueueSize),
		exitCh:              make(chan struct{}),
		startCh:             make(chan struct{}, 1),
		onlineCh:            make(chan struct{}, 1),
		emptyCh:             make(chan struct{}),
		cacheHeight:         new(big.Int),
		targetWeightBalance: new(big.Int),
		isEmpty:             false,
		resubmitIntervalCh:  make(chan time.Duration),
		resubmitAdjustCh:    make(chan *intervalAdjust, resubmitAdjustChanSize),
		cerytify:            NewCertify(ethcrypto.PubkeyToAddress(eth.GetNodeKey().PublicKey), eth, handler),
		miner:               handler,
		notifyBlockCh:       make(chan *types.OnlineValidatorList, 1),
		emptyTimestamp:      time.Now().Unix(),
		emptyHandleFlag:     false,
		resetEmptyCh:        make(chan struct{}, 1),
		totalCondition:      0,
	}

	if _, ok := engine.(consensus.Istanbul); ok || !chainConfig.IsQuorum || chainConfig.Clique != nil {
		// Subscribe NewTxsEvent for tx pool
		worker.txsSub = eth.TxPool().SubscribeNewTxsEvent(worker.txsCh)
		// Subscribe events for blockchain
		worker.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)
		worker.chainSideSub = eth.BlockChain().SubscribeChainSideEvent(worker.chainSideCh)

		// Sanitize recommit interval if the user-specified one is too short.
		recommit := worker.config.Recommit
		if recommit < minRecommitInterval {
			log.Warn("Sanitizing miner recommit interval", "provided", recommit, "updated", minRecommitInterval)
			recommit = minRecommitInterval
		}

		go worker.emptyLoop()
		go worker.mainLoop()
		go worker.newWorkLoop(recommit)
		go worker.resultLoop()
		go worker.taskLoop()

		// Enable worker message processing
		go worker.cerytify.Start()

		// Submit first work to initialize pending state.
		if init {
			worker.startCh <- struct{}{}
		}
	}

	return worker
}

// setEtherbase sets the etherbase used to initialize the block coinbase field.
func (w *worker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

func (w *worker) setGasCeil(ceil uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.config.GasCeil = ceil
}

// setExtra sets the content used to initialize the block extra field.
func (w *worker) setExtra(extra []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.extra = extra
}

// setRecommitInterval updates the interval for miner sealing work recommitting.
func (w *worker) setRecommitInterval(interval time.Duration) {
	w.resubmitIntervalCh <- interval
}

// disablePreseal disables pre-sealing mining feature
func (w *worker) disablePreseal() {
	atomic.StoreUint32(&w.noempty, 1)
}

// enablePreseal enables pre-sealing mining feature
func (w *worker) enablePreseal() {
	atomic.StoreUint32(&w.noempty, 0)
}

// pending returns the pending state and corresponding block.
func (w *worker) pending() (*types.Block, *state.StateDB) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	if w.snapshotState == nil {
		return nil, nil
	}
	return w.snapshotBlock, w.snapshotState.Copy()
}

// pendingBlock returns pending block.
func (w *worker) pendingBlock() *types.Block {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock
}

// pendingBlockAndReceipts returns pending block and corresponding receipts.
func (w *worker) pendingBlockAndReceipts() (*types.Block, types.Receipts) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock, w.snapshotReceipts
}

// start sets the running status as 1 and triggers new work submitting.
func (w *worker) start() {
	atomic.StoreInt32(&w.running, 1)
	if istanbul, ok := w.engine.(consensus.Istanbul); ok {
		istanbul.Start(w.chain, w.chain.CurrentBlock, rawdb.HasBadBlock)
	}
	w.startCh <- struct{}{}
}

// stop sets the running status as 0.
func (w *worker) stop() {
	if istanbul, ok := w.engine.(consensus.Istanbul); ok {
		istanbul.Stop()
	} else {
		fmt.Println("======================", ok)
	}
	atomic.StoreInt32(&w.running, 0)
}

// isRunning returns an indicator whether worker is running or not.
func (w *worker) isRunning() bool {
	return atomic.LoadInt32(&w.running) == 1
}

// close terminates all background threads maintained by the worker.
// Note the worker does not support being closed multiple times.
func (w *worker) close() {
	if w.current != nil && w.current.state != nil {
		w.current.state.StopPrefetcher()
	}
	atomic.StoreInt32(&w.running, 0)
	close(w.exitCh)
}

func (w *worker) resetEmptyCondition() {
	w.isEmpty = false
	w.emptyTimestamp = time.Now().Unix()
	w.totalCondition = 0
	w.emptyTimer.Reset(1 * time.Second)

	w.cerytify.voteIndex = 0
	w.cerytify.round = 0
	w.cerytify.selfMessages.Purge()
}

// recalcRecommit recalculates the resubmitting interval upon feedback.
func recalcRecommit(minRecommit, prev time.Duration, target float64, inc bool) time.Duration {
	var (
		prevF = float64(prev.Nanoseconds())
		next  float64
	)
	if inc {
		next = prevF*(1-intervalAdjustRatio) + intervalAdjustRatio*(target+intervalAdjustBias)
		max := float64(maxRecommitInterval.Nanoseconds())
		if next > max {
			next = max
		}
	} else {
		next = prevF*(1-intervalAdjustRatio) + intervalAdjustRatio*(target-intervalAdjustBias)
		min := float64(minRecommit.Nanoseconds())
		if next < min {
			next = min
		}
	}
	return time.Duration(int64(next))
}

type StartEmptyBlockEvent struct {
	BlockNumber *big.Int
}

//type DoneEmptyBlockEvent struct{}

func (w *worker) emptyLoop() {
	w.emptyTimer = time.NewTimer(0)
	defer w.emptyTimer.Stop()
	<-w.emptyTimer.C // discard the initial tick
	w.emptyTimer.Reset(120 * time.Second)

	gossipTimer := time.NewTimer(0)
	defer gossipTimer.Stop()
	<-gossipTimer.C // discard the initial tick
	gossipTimer.Reset(5 * time.Second)

	checkTimer := time.NewTimer(0)
	defer checkTimer.Stop()
	<-checkTimer.C // discard the initial tick
	checkTimer.Reset(1 * time.Second)

	var valiTotal int

	for {
		select {
		case <-w.resetEmptyCh:
			w.resetEmptyCondition()
		case <-checkTimer.C:
			//log.Info("checkTimer.C", "no", w.chain.CurrentHeader().Number, "w.isEmpty", w.isEmpty)
			checkTimer.Reset(1 * time.Second)
			if !w.isEmpty {
				continue
			}
			//log.Info("checkTimer.C", "w.cacheHeight", w.cacheHeight, "w.chain.CurrentHeader().Number", w.chain.CurrentHeader().Number)
			if w.cacheHeight.Cmp(w.chain.CurrentHeader().Number) <= 0 {
				w.resetEmptyCondition()
				//w.isEmpty = false
				//w.emptyTimestamp = time.Now().Unix()
				////w.emptyTimer.Reset(120 * time.Second)
				//totalCondition = 0
				//w.emptyTimer.Reset(1 * time.Second)
				//w.resetEmptyCh <- struct{}{}
			}

		case <-w.emptyTimer.C:
			{
				w.emptyTimer.Reset(1 * time.Second)
				if !w.isRunning() {
					w.emptyTimestamp = time.Now().Unix()
					continue
				}
				if w.isEmpty {
					continue
				}
				/*
					if time.Now().Unix()-w.emptyTimestamp < 120 {
						continue
					}
				*/
				curTime := time.Now().Unix()
				curBlock := w.chain.CurrentBlock()
				w.totalCondition++

				//log.Info("azh|onlinesLen", "len", len(w.engine.OnlineValidators(w.cacheHeight.Uint64())))

				rs, err := w.chain.IsValidatorByHight(w.chain.CurrentHeader(), w.cerytify.self)
				if err == nil && rs {
					valiTotal = 15
				} else {
					valiTotal = 16
				}

				//if curTime-int64(curBlock.Time()) < 120 && curBlock.Number().Uint64() > 0 {
				if w.totalCondition < 120 && curBlock.Number().Uint64() > 0 {
					//log.Info("wait empty condition", "totalCondition", totalCondition, "time", curTime, "blocktime", int64(w.chain.CurrentBlock().Time()))
					if w.totalCondition != valiTotal {
						continue
					}
					log.Info("len(w.engine.OnlineValidators(curBlock.Number().Uint64()+1))", "len", len(w.engine.OnlineValidators(curBlock.Number().Uint64()+1)), "height", curBlock.Number().Uint64()+1)
					if len(w.engine.OnlineValidators(curBlock.Number().Uint64()+1)) >= 7 {
						continue
					}
					//log.Info("ok empty condition 15", "totalCondition", totalCondition, "time", curTime, "blocktime", int64(w.chain.CurrentBlock().Time()), "online len",len(w.engine.OnlineValidators(curBlock.Number().Uint64()+1)) )
				} else {
					log.Info("ok empty condition 120", "height", new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1)), "totalCondition", w.totalCondition, "time", curTime, "blocktime", int64(w.chain.CurrentBlock().Time()), "online len", len(w.engine.OnlineValidators(curBlock.Number().Uint64()+1)))
				}
				w.totalCondition = 0

				stakes, err := w.chain.ReadValidatorPool(w.chain.CurrentHeader())
				if err != nil {
					log.Error("emptyTimer.C : invalid validtor list", "no", w.chain.CurrentBlock().NumberU64())
					continue
				}

				//for _, val := range w.engine.OnlineValidators(w.cacheHeight.Uint64()) {
				//	log.Info("azh|onlinesAddr", "addr", stakes.GetValidatorAddr(val))
				//}

				//v11, _ := w.eth.BlockChain().Random11ValidatorFromPool(w.chain.CurrentBlock().Header())
				//for _, val := range v11.Validators {
				//	log.Info("azh|empty", "v11", stakes.GetValidatorAddr(val.Addr))
				//}

				//log.Info("emptyLoop", "validators.len", len(stakes.Validators), "stake", w.cerytify.addr)
				if stakes.GetValidatorAddr(w.cerytify.self) == (common.Address{}) {
					w.emptyTimer.Stop()
					continue
				}
				w.cerytify.stakers = stakes

				w.emptyCh <- struct{}{}
				//log.Info("generate block time out", "height", w.current.header.Number, "staker:", w.cerytify.stakers)
				//w.cerytify.lock.Lock()
				//w.cerytify.lock.Unlock()

				if !w.emptyHandleFlag {
					w.emptyHandleFlag = true
					go w.cerytify.handleEvents()
				}

				EmptyEvent := StartEmptyBlockEvent{
					BlockNumber: new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1)),
				}
				err = w.mux.Post(EmptyEvent)
				if err != nil {
					//log.Error("emptyTimer.C : post empty event", "err", err)
					continue
				}

				w.cacheHeight = new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1))

				totalWeightBalance, err := w.targetSizeWithWeight()

				if err != nil {
					log.Error("emptyTimer.C : get targetWeightBalance error", "current block number", w.chain.CurrentBlock().NumberU64())
					continue
				}
				w.targetWeightBalance = totalWeightBalance

				w.isEmpty = true
				//w.onlineCh <- struct{}{}
				w.emptyTimer.Stop()

				if valiTotal == 15 {
					w.cerytify.AssembleAndBroadcastMessage(new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1)))
					gossipTimer.Reset(time.Second * 5)
				}
				//log.Info("emptyLoop start empty")
			}

		case <-gossipTimer.C:
			{
				//log.Info("emptyLoop gossipTimer", "w.isEmpty", w.isEmpty)
				gossipTimer.Reset(time.Second * 5)
				if !w.isEmpty {
					continue
				}
				if w.cerytify.stakers == nil {
					log.Info("emptyLoop", "nil", w.cerytify.stakers == nil)
					continue
				}
				w.cerytify.AssembleAndBroadcastMessage(new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1)))
			}

		case rs := <-w.cerytify.signatureResultCh:
			{
				log.Info("emptyLoop.signatureResultCh", "isEmpty", w.isEmpty, "receiveValidatorsSum:", rs.ReceiveSum, "w.TargetSize()", w.targetWeightBalance, "w.cacheHeight", new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1)), "msgHeight", rs.Height)
				if w.isEmpty && new(big.Int).Add(w.chain.CurrentHeader().Number, big.NewInt(1)).Cmp(rs.Height) == 0 && rs.ReceiveSum.Cmp(w.targetWeightBalance) > 0 {
					//for _, val := range rs.OnlineValidators {
					//	log.Info("azh|empty", "vote", val)
					//}

					log.Info("emptyLoop.start produce empty block", "time", time.Now())
					if err := w.commitEmptyWork(nil, true, time.Now().Unix(), rs.OnlineValidators, rs.EmptyMessages); err != nil {
						log.Error("emptyLoop.commitEmptyWork error", "err", err)
					} else {
						w.resetEmptyCondition()
						//w.resetEmptyCh <- struct{}{}
					}
					//sgiccommon.Sigc <- syscall.SIGTERM
				}
				w.cerytify.proofStatePool.ClearPrev(w.chain.CurrentHeader().Number)
			}
		}
	}
}

// newWorkLoop is a standalone goroutine to submit new mining work upon received events.
func (w *worker) newWorkLoop(recommit time.Duration) {
	var (
		interrupt   *int32
		minRecommit = recommit // minimal resubmit interval specified by user.
		timestamp   int64      // timestamp for each round of mining.
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	proofTimer := time.NewTimer(0)
	defer proofTimer.Stop()
	<-proofTimer.C // discard the initial tick

	// commit aborts in-flight transaction execution with given signal and resubmits a new one.
	commit := func(noempty bool, s int32) {
		if interrupt != nil {
			atomic.StoreInt32(interrupt, s)
		}
		interrupt = new(int32)
		select {
		case w.newWorkCh <- &newWorkReq{interrupt: interrupt, noempty: noempty, timestamp: timestamp}:
		case <-w.exitCh:
			return
		}
		timer.Reset(recommit)
		atomic.StoreInt32(&w.newTxs, 0)
	}
	// clearPending cleans the stale pending tasks.
	clearPending := func(number uint64) {
		w.pendingMu.Lock()
		for h, t := range w.pendingTasks {
			if t.block.NumberU64()+staleThreshold <= number {
				delete(w.pendingTasks, h)
			}
		}
		w.pendingMu.Unlock()
	}

	for {
		select {
		case <-w.startCh:
			clearPending(w.chain.CurrentBlock().NumberU64())
			timestamp = time.Now().Unix()
			log.Info("w.startCh", "no", w.chain.CurrentBlock().NumberU64()+1)
			commit(false, commitInterruptNewHead)
		case head := <-w.chainHeadCh:
			if w.cacheHeight.Cmp(head.Block.Number()) <= 0 {
				// modification on 20221102 start
				//if w.isEmpty {
				//	w.mux.Post(DoneEmptyBlockEvent{})
				//}
				// modification on 20221102 end
				log.Info("w.chainHeadCh: reset empty timer", "no", head.Block.NumberU64())
				//w.isEmpty = false
				//w.emptyTimestamp = time.Now().Unix()
				//w.emptyTimer.Reset(120 * time.Second)
				w.resetEmptyCh <- struct{}{}
			}
			log.Info("w.chainHeadCh: start commit block", "no", head.Block.NumberU64())

			if w.isRunning() {
				w.emptyTimestamp = time.Now().Unix()
			}

			log.Info("w.chainHeadCh", "no", head.Block.Number().Uint64()+1)
			if h, ok := w.engine.(consensus.Handler); ok {
				h.NewChainHead()
			}
			clearPending(head.Block.NumberU64())
			timestamp = time.Now().Unix()
			// Start submitting online proof blocks
			w.onlineValidators = nil
			commit(false, commitInterruptNewHead)

		case onlineValidators := <-w.notifyBlockCh:
			if onlineValidators != nil {
				log.Info("w.notifyBlockCh", "height", w.chain.CurrentHeader().Number.Uint64()+1)
				w.onlineValidators = onlineValidators
				// clearPending(w.chain.CurrentHeader().Number.Uint64() + 1)
				// timestamp = time.Now().Unix()
				// commit(false, commitInterruptNewHead)
			}

		case <-timer.C:
			// If mining is running resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() {
				log.Info("timer.C : commit request", "no", w.chain.CurrentHeader().Number.Uint64()+1)
				commit(false, commitInterruptResubmit)
			}
			//log.Info("timer.C : commit request", "no", w.chain.CurrentHeader().Number.Uint64()+1, "w.isRunning", w.isRunning())
			//commit(false, commitInterruptResubmit)

		case interval := <-w.resubmitIntervalCh:
			// Adjust resubmit interval explicitly by user.
			if interval < minRecommitInterval {
				log.Warn("Sanitizing miner recommit interval", "provided", interval, "updated", minRecommitInterval)
				interval = minRecommitInterval
			}
			log.Info("Miner recommit interval update", "from", minRecommit, "to", interval)
			minRecommit, recommit = interval, interval

			if w.resubmitHook != nil {
				w.resubmitHook(minRecommit, recommit)
			}

		case adjust := <-w.resubmitAdjustCh:
			// Adjust resubmit interval by feedback.
			if adjust.inc {
				before := recommit
				target := float64(recommit.Nanoseconds()) / adjust.ratio
				recommit = recalcRecommit(minRecommit, recommit, target, true)
				log.Trace("Increase miner recommit interval", "from", before, "to", recommit)
			} else {
				before := recommit
				recommit = recalcRecommit(minRecommit, recommit, float64(minRecommit.Nanoseconds()), false)
				log.Trace("Decrease miner recommit interval", "from", before, "to", recommit)
			}

			if w.resubmitHook != nil {
				w.resubmitHook(minRecommit, recommit)
			}

		case <-w.exitCh:
			return
		}
	}
}

// mainLoop is a standalone goroutine to regenerate the sealing task based on the received event.
func (w *worker) mainLoop() {
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()
	defer w.chainSideSub.Unsubscribe()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitNewWork(req.interrupt, req.noempty, req.timestamp)

		case ev := <-w.chainSideCh:
			log.Info("w.chainSideCh", "height", ev.Block.NumberU64())
			// Short circuit for duplicate side blocks
			if _, exist := w.localUncles[ev.Block.Hash()]; exist {
				continue
			}
			if _, exist := w.remoteUncles[ev.Block.Hash()]; exist {
				continue
			}
			// Add side block to possible uncle block set depending on the author.
			if w.isLocalBlock != nil && w.isLocalBlock(ev.Block) {
				w.localUncles[ev.Block.Hash()] = ev.Block
			} else {
				w.remoteUncles[ev.Block.Hash()] = ev.Block
			}
			// If our mining block contains less than 2 uncle blocks,
			// add the new uncle block if valid and regenerate a mining block.
			if w.isRunning() && w.current != nil && w.current.uncles.Cardinality() < 2 {
				start := time.Now()
				if err := w.commitUncle(w.current, ev.Block.Header()); err == nil {
					var uncles []*types.Header
					w.current.uncles.Each(func(item interface{}) bool {
						hash, ok := item.(common.Hash)
						if !ok {
							return false
						}
						uncle, exist := w.localUncles[hash]
						if !exist {
							uncle, exist = w.remoteUncles[hash]
						}
						if !exist {
							return false
						}
						uncles = append(uncles, uncle.Header())
						return false
					})
					w.commit(uncles, nil, true, start)
				}
			}

		case ev := <-w.txsCh:
			// Apply transactions to the pending state if we're not mining.
			//
			// Note all transactions received may not be continuous with transactions
			// already included in the current mining block. These transactions will
			// be automatically eliminated.
			if !w.isRunning() {
				continue
			}
			if !w.isRunning() && w.current != nil {
				// If block is already full, abort
				if gp := w.current.gasPool; gp != nil && gp.Gas() < params.TxGas {
					continue
				}
				w.mu.RLock()
				coinbase := w.coinbase
				w.mu.RUnlock()

				txs := make(map[common.Address]types.Transactions)
				for _, tx := range ev.Txs {
					acc, _ := types.Sender(w.current.signer, tx)
					txs[acc] = append(txs[acc], tx)
				}
				txset := types.NewTransactionsByPriceAndNonce(w.current.signer, txs, w.current.header.BaseFee)
				tcount := w.current.tcount
				log.Info("caver|w.txsCh|commitTransactions", "no", w.current.header.Number.Uint64())
				w.commitTransactions(txset, coinbase, nil)
				// Only update the snapshot if any new transactons were added
				// to the pending block
				if tcount != w.current.tcount {
					w.updateSnapshot()
				}
			} else {
				// Special case, if the consensus engine is 0 period clique(dev mode),
				// submit mining work here since all empty submission will be rejected
				// by clique. Of course the advance sealing(empty submission) is disabled.
				if w.chainConfig.Clique != nil && w.chainConfig.Clique.Period == 0 {
					log.Info("w.chainConfig.Clique != nil && w.chainConfig.Clique.Period == 0")
					w.commitNewWork(nil, true, time.Now().Unix())
				}
			}
			atomic.AddInt32(&w.newTxs, int32(len(ev.Txs)))

		// System stopped
		case <-w.exitCh:
			return
		case <-w.txsSub.Err():
			return
		case <-w.chainHeadSub.Err():
			return
		case <-w.chainSideSub.Err():
			return
		}
	}
}

// taskLoop is a standalone goroutine to fetch sealing task from the generator and
// push them to consensus engine.
func (w *worker) taskLoop() {
	var (
		stopCh chan struct{}
		prev   common.Hash
	)

	// interrupt aborts the in-flight sealing task.
	interrupt := func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	for {
		select {
		case task := <-w.taskCh:
			if w.isEmpty {
				continue
			}
			if task.block.Coinbase() == (common.Address{}) {
				log.Info("w.taskch", "no", task.block.NumberU64())
				continue
			}
			if w.newTaskHook != nil {
				w.newTaskHook(task)
			}
			// Reject duplicate sealing work due to resubmitting.
			sealHash := w.engine.SealHash(task.block.Header())
			if sealHash == prev {
				continue
			}
			// Interrupt previous sealing operation
			interrupt()
			stopCh, prev = make(chan struct{}), sealHash

			if w.skipSealHook != nil && w.skipSealHook(task) {
				continue
			}
			w.pendingMu.Lock()
			w.pendingTasks[sealHash] = task
			w.pendingMu.Unlock()
			if err := w.engine.Seal(w.chain, task.block, w.resultCh, stopCh); err != nil {
				log.Warn("Block sealing failed", "err", err, "no", task.block.NumberU64(), "hash", task.block.Hash())
			}
		case <-w.exitCh:
			interrupt()
			return
		case <-w.emptyCh:
			interrupt()
			log.Info("emptyCh interrupt")
		}
	}
}

// resultLoop is a standalone goroutine to handle sealing result submitting
// and flush relative data to the database.
func (w *worker) resultLoop() {
	for {
		select {
		case block := <-w.resultCh:
			// Short circuit when receiving empty result.
			if block == nil {
				continue
			}
			// Short circuit when receiving duplicate result caused by resubmitting.
			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
				log.Info("caver|resultLoop|HasBlock", "no", block.NumberU64())
				continue
			}
			var (
				sealhash = w.engine.SealHash(block.Header())
				hash     = block.Hash()
			)
			w.pendingMu.RLock()
			task, exist := w.pendingTasks[sealhash]
			w.pendingMu.RUnlock()
			if !exist {
				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash, "hash", hash)
				continue
			}
			// Different block could share same sealhash, deep copy here to prevent write-write conflict.
			var (
				receipts = make([]*types.Receipt, len(task.receipts))
				logs     []*types.Log
			)
			for i, receipt := range task.receipts {
				// add block location fields
				receipt.BlockHash = hash
				receipt.BlockNumber = block.Number()
				receipt.TransactionIndex = uint(i)

				receipts[i] = new(types.Receipt)
				*receipts[i] = *receipt
				// Update the block hash in all logs since it is now available and not when the
				// receipt/log of individual transactions were created.
				for _, log := range receipt.Logs {
					log.BlockHash = hash
				}
				logs = append(logs, receipt.Logs...)
			}
			// Commit block and state to database.
			_, err := w.chain.WriteBlockWithState(block, receipts, logs, task.state, true)
			if err != nil {
				log.Error("Failed writing block to chain", "err", err)
				continue
			}
			log.Info("Successfully sealed new block", "number", block.Number(), "sealhash", sealhash, "hash", hash,
				"elapsed", common.PrettyDuration(time.Since(task.createdAt)))
			// Broadcast the block and announce chain insertion event
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

			// Insert the block into the set of pending ones to resultLoop for confirmations
			w.unconfirmed.Insert(block.NumberU64(), block.Hash())

		case <-w.exitCh:
			return
		}
	}
}

// makeEmptyCurrent creates a new environment for the current cycle.
func (w *worker) makeEmptyCurrent(parent *types.Block, header *types.Header) error {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit
	state, err := w.chain.StateAt(parent.Root())
	if err != nil {
		return err
	}
	state.StartPrefetcher("miner")

	var mintDeep *types.MintDeep
	//var exchangeList *types.SNFTExchangeList
	if parent.NumberU64() > 0 {
		mintDeep, err = w.chain.ReadMintDeep(parent.Header())
		if err != nil {
			log.Error("Failed get mintdeep ", "err", err)
			return err
		}
		//exchangeList, _ = w.chain.ReadSNFTExchangePool(parent.Header())
		//if exchangeList == nil {
		//	exchangeList = &types.SNFTExchangeList{
		//		SNFTExchanges: make([]*types.SNFTExchange, 0),
		//	}
		//}

	} else {
		mintDeep = new(types.MintDeep)
		//mintDeep.OfficialMint = big.NewInt(1)
		//
		//mintDeep.UserMint = big.NewInt(0)
		//maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
		//mintDeep.UserMint.Add(big.NewInt(1), maskB)
		mintDeep.UserMint = big.NewInt(1)

		mintDeep.OfficialMint = big.NewInt(0)
		maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
		mintDeep.OfficialMint.Add(big.NewInt(0), maskB)

		//exchangeList = &types.SNFTExchangeList{
		//	SNFTExchanges: make([]*types.SNFTExchange, 0),
		//}
	}
	state.MintDeep = mintDeep
	//state.SNFTExchangePool = exchangeList

	officialNFTList, _ := w.chain.ReadOfficialNFTPool(parent.Header())
	state.OfficialNFTPool = officialNFTList
	for _, v := range state.OfficialNFTPool.InjectedOfficialNFTs {
		log.Info("makeCurrent()", "state.OfficialNFTPool.InjectedOfficialNFTs", v)
	}

	var nominatedOfficialNFT *types.NominatedOfficialNFT
	if parent.NumberU64() > 0 {
		nominatedOfficialNFT, err = w.chain.ReadNominatedOfficialNFT(parent.Header())
		if err != nil {
			state.NominatedOfficialNFT = nil
		} else {
			state.NominatedOfficialNFT = nominatedOfficialNFT
		}
	} else {
		nominatedOfficialNFT = new(types.NominatedOfficialNFT)
		nominatedOfficialNFT.Dir = types.DefaultDir
		nominatedOfficialNFT.StartIndex = new(big.Int).Set(state.OfficialNFTPool.MaxIndex())
		nominatedOfficialNFT.Number = types.DefaultNumber
		nominatedOfficialNFT.Royalty = types.DefaultRoyalty
		nominatedOfficialNFT.Creator = types.DefaultCreator
		nominatedOfficialNFT.Address = common.Address{}
		state.NominatedOfficialNFT = nominatedOfficialNFT
	}

	vallist, err := w.chain.ReadValidatorPool(parent.Header())
	if err != nil {
		log.Error("makeEmptyCurrent : invalid validator list", "no", header.Number, "err", err)
		return err
	}

	state.ValidatorPool = vallist.Validators

	env := &environment{
		signer:    types.MakeSigner(w.chainConfig, header.Number),
		state:     state,
		ancestors: mapset.NewSet(),
		family:    mapset.NewSet(),
		uncles:    mapset.NewSet(),
		header:    header,
	}
	// when 08 is processed ancestors contain 07 (quick block)
	for _, ancestor := range w.chain.GetBlocksFromHash(parent.Hash(), 7) {
		for _, uncle := range ancestor.Uncles() {
			env.family.Add(uncle.Hash())
		}
		env.family.Add(ancestor.Hash())
		env.ancestors.Add(ancestor.Hash())
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0

	// Swap out the old work with the new one, terminating any leftover prefetcher
	// processes in the mean time and starting a new one.
	if w.emptycurrent != nil && w.emptycurrent.state != nil {
		w.emptycurrent.state.StopPrefetcher()
	}
	w.emptycurrent = env
	return nil
}

// makeCurrent creates a new environment for the current cycle.
func (w *worker) makeCurrent(parent *types.Block, header *types.Header) error {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit
	state, err := w.chain.StateAt(parent.Root())
	if err != nil {
		return err
	}
	state.StartPrefetcher("miner")

	var mintDeep *types.MintDeep
	//var exchangeList *types.SNFTExchangeList
	if parent.NumberU64() > 0 {
		mintDeep, err = w.chain.ReadMintDeep(parent.Header())
		if err != nil {
			log.Error("Failed get mintdeep ", "err", err)
			return err
		}
		//exchangeList, _ = w.chain.ReadSNFTExchangePool(parent.Header())
		//if exchangeList == nil {
		//	exchangeList = &types.SNFTExchangeList{
		//		SNFTExchanges: make([]*types.SNFTExchange, 0),
		//	}
		//}

	} else {
		mintDeep = new(types.MintDeep)
		//mintDeep.OfficialMint = big.NewInt(1)
		//
		//mintDeep.UserMint = big.NewInt(0)
		//maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
		//mintDeep.UserMint.Add(big.NewInt(1), maskB)
		mintDeep.UserMint = big.NewInt(1)

		mintDeep.OfficialMint = big.NewInt(0)
		maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
		mintDeep.OfficialMint.Add(big.NewInt(0), maskB)

		//exchangeList = &types.SNFTExchangeList{
		//	SNFTExchanges: make([]*types.SNFTExchange, 0),
		//}
	}
	state.MintDeep = mintDeep
	//state.SNFTExchangePool = exchangeList

	officialNFTList, _ := w.chain.ReadOfficialNFTPool(parent.Header())
	state.OfficialNFTPool = officialNFTList
	for _, v := range state.OfficialNFTPool.InjectedOfficialNFTs {
		log.Info("makeCurrent()", "state.OfficialNFTPool.InjectedOfficialNFTs", v)
	}

	var nominatedOfficialNFT *types.NominatedOfficialNFT
	if parent.NumberU64() > 0 {
		nominatedOfficialNFT, err = w.chain.ReadNominatedOfficialNFT(parent.Header())
		if err != nil {
			state.NominatedOfficialNFT = nil
		} else {
			state.NominatedOfficialNFT = nominatedOfficialNFT
		}
	} else {
		nominatedOfficialNFT = new(types.NominatedOfficialNFT)
		nominatedOfficialNFT.Dir = types.DefaultDir
		nominatedOfficialNFT.StartIndex = new(big.Int).Set(state.OfficialNFTPool.MaxIndex())
		nominatedOfficialNFT.Number = types.DefaultNumber
		nominatedOfficialNFT.Royalty = types.DefaultRoyalty
		nominatedOfficialNFT.Creator = types.DefaultCreator
		nominatedOfficialNFT.Address = common.Address{}
		state.NominatedOfficialNFT = nominatedOfficialNFT
	}

	vallist, err := w.chain.ReadValidatorPool(parent.Header())
	if err != nil {
		log.Error("makeCurrent : invalid validator list", "no", header.Number, "err", err)
		return err
	}
	state.ValidatorPool = vallist.Validators

	env := &environment{
		signer:    types.MakeSigner(w.chainConfig, header.Number),
		state:     state,
		ancestors: mapset.NewSet(),
		family:    mapset.NewSet(),
		uncles:    mapset.NewSet(),
		header:    header,
	}
	// when 08 is processed ancestors contain 07 (quick block)
	for _, ancestor := range w.chain.GetBlocksFromHash(parent.Hash(), 7) {
		for _, uncle := range ancestor.Uncles() {
			env.family.Add(uncle.Hash())
		}
		env.family.Add(ancestor.Hash())
		env.ancestors.Add(ancestor.Hash())
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0

	// Swap out the old work with the new one, terminating any leftover prefetcher
	// processes in the mean time and starting a new one.
	if w.current != nil && w.current.state != nil {
		w.current.state.StopPrefetcher()
	}
	w.current = env
	return nil
}

// commitUncle adds the given block to uncle block set, returns error if failed to add.
func (w *worker) commitUncle(env *environment, uncle *types.Header) error {
	hash := uncle.Hash()
	if env.uncles.Contains(hash) {
		return errors.New("uncle not unique")
	}
	if env.header.ParentHash == uncle.ParentHash {
		return errors.New("uncle is sibling")
	}
	// if !env.ancestors.Contains(uncle.ParentHash) {
	// 	return errors.New("uncle's parent unknown")
	// }
	if env.family.Contains(hash) {
		return errors.New("uncle already included")
	}
	env.uncles.Add(uncle.Hash())

	// Record bad behavior to local database
	w.RecordEvilAction(uncle)

	return nil
}

func (w *worker) RecordEvilAction(uncle *types.Header) {
	if uncle.Coinbase == (common.Address{}) { // do not handle empty block forks
		return
	}

	evilAction, err := w.eth.BlockChain().ReadEvilAction(uncle.Number.Uint64())
	if err != nil {
		log.Error("err read evil action", "err", err.Error())
		return
	}

	if evilAction != nil && !evilAction.Handled && !evilAction.Exist(uncle) {
		evilAction.EvilHeaders = append(evilAction.EvilHeaders, uncle)
	}

	if evilAction == nil {
		evilAction = types.NewEvilAction(uncle)
	}

	w.eth.BlockChain().WriteEvilAction(uncle.Number.Uint64(), *evilAction)
}

// updateSnapshot updates pending snapshot block and state.
// Note this function assumes the current variable is thread safe.
func (w *worker) updateSnapshot() {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	var uncles []*types.Header
	w.current.uncles.Each(func(item interface{}) bool {
		hash, ok := item.(common.Hash)
		if !ok {
			return false
		}
		uncle, exist := w.localUncles[hash]
		if !exist {
			uncle, exist = w.remoteUncles[hash]
		}
		if !exist {
			return false
		}
		uncles = append(uncles, uncle.Header())
		return false
	})

	w.snapshotBlock = types.NewBlock(
		w.current.header,
		w.current.txs,
		uncles,
		w.current.receipts,
		trie.NewStackTrie(nil),
	)
	w.snapshotReceipts = copyReceipts(w.current.receipts)
	w.snapshotState = w.current.state.Copy()
}

func (w *worker) updateEmptySnapshot() {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	var uncles []*types.Header
	//w.current.uncles.Each(func(item interface{}) bool {
	//	hash, ok := item.(common.Hash)
	//	if !ok {
	//		return false
	//	}
	//	uncle, exist := w.localUncles[hash]
	//	if !exist {
	//		uncle, exist = w.remoteUncles[hash]
	//	}
	//	if !exist {
	//		return false
	//	}
	//	uncles = append(uncles, uncle.Header())
	//	return false
	//})

	w.snapshotBlock = types.NewBlock(
		w.emptycurrent.header,
		w.emptycurrent.txs,
		uncles,
		w.emptycurrent.receipts,
		trie.NewStackTrie(nil),
	)
	w.snapshotReceipts = copyReceipts(w.emptycurrent.receipts)
	w.snapshotState = w.emptycurrent.state.Copy()
}

func (w *worker) commitTransactionForEmpty(tx *types.Transaction, coinbase common.Address) ([]*types.Log, error) {
	snap := w.emptycurrent.state.Snapshot()

	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &coinbase, w.emptycurrent.gasPool, w.emptycurrent.state, w.emptycurrent.header, tx, &w.emptycurrent.header.GasUsed, *w.chain.GetVMConfig())
	if err != nil {
		w.emptycurrent.state.RevertToSnapshot(snap)
		return nil, err
	}
	w.emptycurrent.txs = append(w.emptycurrent.txs, tx)
	w.emptycurrent.receipts = append(w.emptycurrent.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) commitTransaction(tx *types.Transaction, coinbase common.Address) ([]*types.Log, error) {
	snap := w.current.state.Snapshot()

	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &coinbase, w.current.gasPool, w.current.state, w.current.header, tx, &w.current.header.GasUsed, *w.chain.GetVMConfig())
	if err != nil {
		w.current.state.RevertToSnapshot(snap)
		return nil, err
	}
	w.current.txs = append(w.current.txs, tx)
	w.current.receipts = append(w.current.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) commitTransactionsForEmpty(txs *types.TransactionsByPriceAndNonce, coinbase common.Address, interrupt *int32) bool {
	// Short circuit if current is nil
	if w.emptycurrent == nil {
		return true
	}

	gasLimit := w.emptycurrent.header.GasLimit
	if w.emptycurrent.gasPool == nil {
		w.emptycurrent.gasPool = new(core.GasPool).AddGas(gasLimit)
	}

	var coalescedLogs []*types.Log

	for {
		// In the following three cases, we will interrupt the execution of the transaction.
		// (1) new head block event arrival, the interrupt signal is 1
		// (2) worker start or restart, the interrupt signal is 1
		// (3) worker recreate the mining block with any newly arrived transactions, the interrupt signal is 2.
		// For the first two cases, the semi-finished work will be discarded.
		// For the third case, the semi-finished work will be submitted to the consensus engine.
		if interrupt != nil && atomic.LoadInt32(interrupt) != commitInterruptNone {
			// Notify resubmit loop to increase resubmitting interval due to too frequent commits.
			if atomic.LoadInt32(interrupt) == commitInterruptResubmit {
				ratio := float64(gasLimit-w.emptycurrent.gasPool.Gas()) / float64(gasLimit)
				if ratio < 0.1 {
					ratio = 0.1
				}
				w.resubmitAdjustCh <- &intervalAdjust{
					ratio: ratio,
					inc:   true,
				}
			}
			return atomic.LoadInt32(interrupt) == commitInterruptNewHead
		}
		// If we don't have enough gas for any further transactions then we're done
		if w.emptycurrent.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", w.current.gasPool, "want", params.TxGas)
			break
		}
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(w.emptycurrent.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(w.emptycurrent.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)

			txs.Pop()
			continue
		}
		// Start executing the transaction
		w.emptycurrent.state.Prepare(tx.Hash(), w.emptycurrent.tcount)

		log.Info("worker|commitTransaction", "no", w.emptycurrent.header.Number.String(), "hash", tx.Hash().Hex())
		logs, err := w.commitTransactionForEmpty(tx, coinbase)
		switch {
		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			w.emptycurrent.tcount++
			txs.Shift()

		case errors.Is(err, core.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}

	if !w.isRunning() && !w.isEmpty && len(coalescedLogs) > 0 {
		// We don't push the pendingLogsEvent while we are mining. The reason is that
		// when we are mining, the worker will regenerate a mining block every 3 seconds.
		// In order to avoid pushing the repeated pendingLog, we disable the pending log pushing.

		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		w.pendingLogsFeed.Send(cpy)
	}
	// Notify resubmit loop to decrease resubmitting interval if current interval is larger
	// than the user-specified one.
	if interrupt != nil {
		w.resubmitAdjustCh <- &intervalAdjust{inc: false}
	}
	return false
}

func (w *worker) commitTransactions(txs *types.TransactionsByPriceAndNonce, coinbase common.Address, interrupt *int32) bool {
	// Short circuit if current is nil
	if w.current == nil {
		return true
	}

	gasLimit := w.current.header.GasLimit
	if w.current.gasPool == nil {
		w.current.gasPool = new(core.GasPool).AddGas(gasLimit)
	}

	var coalescedLogs []*types.Log

	for {
		// In the following three cases, we will interrupt the execution of the transaction.
		// (1) new head block event arrival, the interrupt signal is 1
		// (2) worker start or restart, the interrupt signal is 1
		// (3) worker recreate the mining block with any newly arrived transactions, the interrupt signal is 2.
		// For the first two cases, the semi-finished work will be discarded.
		// For the third case, the semi-finished work will be submitted to the consensus engine.
		if interrupt != nil && atomic.LoadInt32(interrupt) != commitInterruptNone {
			// Notify resubmit loop to increase resubmitting interval due to too frequent commits.
			if atomic.LoadInt32(interrupt) == commitInterruptResubmit {
				ratio := float64(gasLimit-w.current.gasPool.Gas()) / float64(gasLimit)
				if ratio < 0.1 {
					ratio = 0.1
				}
				w.resubmitAdjustCh <- &intervalAdjust{
					ratio: ratio,
					inc:   true,
				}
			}
			return atomic.LoadInt32(interrupt) == commitInterruptNewHead
		}
		// If we don't have enough gas for any further transactions then we're done
		if w.current.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", w.current.gasPool, "want", params.TxGas)
			break
		}
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(w.current.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(w.current.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)

			txs.Pop()
			continue
		}
		// Start executing the transaction
		w.current.state.Prepare(tx.Hash(), w.current.tcount)

		log.Info("worker|commitTransaction", "no", w.current.header.Number.String(), "hash", tx.Hash().Hex())
		logs, err := w.commitTransaction(tx, coinbase)
		switch {
		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			w.current.tcount++
			txs.Shift()

		case errors.Is(err, core.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}

	if !w.isRunning() && len(coalescedLogs) > 0 {
		// We don't push the pendingLogsEvent while we are mining. The reason is that
		// when we are mining, the worker will regenerate a mining block every 3 seconds.
		// In order to avoid pushing the repeated pendingLog, we disable the pending log pushing.

		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		w.pendingLogsFeed.Send(cpy)
	}
	// Notify resubmit loop to decrease resubmitting interval if current interval is larger
	// than the user-specified one.
	if interrupt != nil {
		w.resubmitAdjustCh <- &intervalAdjust{inc: false}
	}
	return false
}

// commitEmptyWork generates several new sealing tasks based on the parent block.
func (w *worker) commitEmptyWork(interrupt *int32, noempty bool, timestamp int64, validators []common.Address, emptyBlockMessages [][]byte) error {
	log.Info("caver|commitEmptyWork|enter", "currentNo", w.chain.CurrentHeader().Number.Uint64())

	if !w.isEmpty {
		return errors.New("w.isEmpty == false")
	}

	w.mu.RLock()
	defer w.mu.RUnlock()
	parent := w.chain.CurrentBlock()
	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit(), w.config.GasCeil),
		Extra:      w.extra,
		Time:       uint64(0),
		BaseFee:    parent.BaseFee(),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if w.chainConfig.IsLondon(header.Number) {
		header.BaseFee = misc.CalcBaseFee(w.chainConfig, parent.Header())
		if !w.chainConfig.IsLondon(parent.Number()) {
			parentGasLimit := parent.GasLimit() * params.ElasticityMultiplier
			header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
		}
	}

	// Only set the coinbase if our consensus engine is running (avoid spurious block rewards)
	if err := w.engine.PrepareForEmptyBlock(w.chain, header, validators, emptyBlockMessages); err != nil {
		log.Error("Failed to prepare header for mining", "err", err)
		return err
	}
	err := w.makeEmptyCurrent(parent, header)
	if err != nil {
		log.Error("Failed to create mining context", "err", err)
		return err
	}
	//receipts := copyReceipts(w.emptycurrent.receipts)

	// Fill the block with all available pending transactions.
	pending, err := w.eth.TxPool().Pending(false)
	if err != nil {
		log.Error("Failed to fetch pending transactions", "err", err)
		return err
	}

	// Split the pending transactions into locals and remotes
	localTxs, remoteTxs := make(map[common.Address]types.Transactions), pending
	for _, account := range w.eth.TxPool().Locals() {
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}

	if len(localTxs) > 0 {
		log.Info("azh|commitNewWork|localTxs", "no", header.Number, "len", len(localTxs))
		txs := types.NewTransactionsByPriceAndNonce(w.emptycurrent.signer, localTxs, header.BaseFee)
		if w.commitTransactionsForEmpty(txs, common.Address{}, interrupt) {
			return xerrors.New("commit transactions err")
		}
	}
	if len(remoteTxs) > 0 {
		log.Info("azh|commitNewWork|remoteTxs", "no", header.Number, "len", len(remoteTxs))
		txs := types.NewTransactionsByPriceAndNonce(w.emptycurrent.signer, remoteTxs, header.BaseFee)
		if w.commitTransactionsForEmpty(txs, common.Address{}, interrupt) {
			return xerrors.New("commit transactions err")
		}
	}

	// Deep copy receipts here
	//to avoid interaction between different tasks.
	receipts := copyReceipts(w.emptycurrent.receipts)
	s := w.emptycurrent.state.Copy()

	block, err := w.engine.FinalizeAndAssemble(w.chain, w.emptycurrent.header, s, w.emptycurrent.txs, nil, receipts)
	//block, err := w.engine.FinalizeAndAssemble(w.chain, w.emptycurrent.header, s, w.emptycurrent.txs, nil, receipts
	if err != nil {
		log.Info("caver|commit|w.engine.FinalizeAndAssemble", "no", w.emptycurrent.header.Number.Uint64(), "err", err.Error())
		return err
	}
	w.updateEmptySnapshot()
	//w.start()
	emptyblock, err := w.engine.SealforEmptyBlock(w.chain, block, validators)
	if err != nil {
		log.Warn("Empty Block sealing failed", "err", err, "no", block.NumberU64(), "hash", block.Hash())
		return err
	}
	hash := emptyblock.Hash()
	var (
		receiptss = make([]*types.Receipt, len(receipts))
		logs      []*types.Log
	)
	for i, receipt := range receipts {
		// add block location fields
		receipt.BlockHash = hash
		receipt.BlockNumber = block.Number()
		receipt.TransactionIndex = uint(i)

		receiptss[i] = new(types.Receipt)
		*receiptss[i] = *receipt
		// Update the block hash in all logs since it is now available and not when the
		// receipt/log of individual transactions were created.
		for _, log := range receipt.Logs {
			log.BlockHash = hash
		}
		logs = append(logs, receipt.Logs...)
	}

	//_, err = w.chain.WriteBlockWithState(emptyblock, receiptss, logs, s, true)
	//if err != nil {
	//	log.Error("commitEmpty Failed writing block to chain", "err", err)
	//	return err
	//}
	//log.Info("empty block wirte to localdb", "Number:", w.emptycurrent.header.Number.Uint64())

	blocks := []*types.Block{emptyblock}
	w.eth.BlockChain().InsertChain(blocks)
	w.mux.Post(core.NewMinedBlockEvent{Block: emptyblock})
	return nil
}

// commitNewWork generates several new sealing tasks based on the parent block.
func (w *worker) commitNewWork(interrupt *int32, noempty bool, timestamp int64) {
	log.Info("caver|commitNewWork|enter", "currentNo", w.chain.CurrentHeader().Number.Uint64())

	if !w.isRunning() {
		return
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	tstart := time.Now()
	parent := w.chain.CurrentBlock()

	if parent.Time() >= uint64(timestamp) {
		timestamp = int64(parent.Time() + 1)
	}
	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit(), w.config.GasCeil),
		Extra:      w.extra,
		Time:       uint64(timestamp),
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if w.chainConfig.IsLondon(header.Number) {
		header.BaseFee = misc.CalcBaseFee(w.chainConfig, parent.Header())
		if !w.chainConfig.IsLondon(parent.Number()) {
			parentGasLimit := parent.GasLimit() * params.ElasticityMultiplier
			header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
		}
	}
	// Only set the coinbase if our consensus engine is running (avoid spurious block rewards)
	if w.isRunning() {
		if w.coinbase == (common.Address{}) {
			log.Error("Refusing to mine without etherbase")
			return
		}
		header.Coinbase = w.coinbase
		if err := w.engine.Prepare(w.chain, header); err != nil {
			log.Error("Failed to prepare header for mining", "err", err)
			return
		}
	}

	// If we are care about TheDAO hard-fork check whether to override the extra-data or not
	if daoBlock := w.chainConfig.DAOForkBlock; daoBlock != nil {
		// Check whether the block is among the fork extra-override range
		limit := new(big.Int).Add(daoBlock, params.DAOForkExtraRange)
		if header.Number.Cmp(daoBlock) >= 0 && header.Number.Cmp(limit) < 0 {
			// Depending whether we support or oppose the fork, override differently
			if w.chainConfig.DAOForkSupport {
				header.Extra = common.CopyBytes(params.DAOForkBlockExtra)
			} else if bytes.Equal(header.Extra, params.DAOForkBlockExtra) {
				header.Extra = []byte{} // If miner opposes, don't let it use the reserved extra-data
			}
		}
	}
	// Could potentially happen if starting to mine in an odd state.
	//deep, err := w.chain.ReadOfficialNFTPool(w.chain.CurrentHeader())
	//fmt.Println("deep", deep, "err", err)
	err := w.makeCurrent(parent, header)
	if err != nil {
		log.Error("Failed to create mining context", "err", err)
		return
	}
	// Create the current work task and check any fork transitions needed
	env := w.current
	if w.chainConfig.DAOForkSupport && w.chainConfig.DAOForkBlock != nil && w.chainConfig.DAOForkBlock.Cmp(header.Number) == 0 {
		misc.ApplyDAOHardFork(env.state)
	}
	// Accumulate the uncles for the current block
	uncles := make([]*types.Header, 0, 2)
	commitUncles := func(blocks map[common.Hash]*types.Block) {
		// Clean up stale uncle blocks first
		for hash, uncle := range blocks {
			if uncle.NumberU64()+staleThreshold <= header.Number.Uint64() {
				delete(blocks, hash)
			}
		}
		for hash, uncle := range blocks {
			if len(uncles) == 2 {
				break
			}
			if err := w.commitUncle(env, uncle.Header()); err != nil {
				log.Trace("Possible uncle rejected", "hash", hash, "reason", err)
			} else {
				log.Debug("Committing new uncle to block", "hash", hash)
				uncles = append(uncles, uncle.Header())
			}
		}
	}
	// Prefer to locally generated uncle
	commitUncles(w.localUncles)
	commitUncles(w.remoteUncles)

	// Create an empty block based on temporary copied state for
	// sealing in advance without waiting block execution finished.
	if !noempty && atomic.LoadUint32(&w.noempty) == 0 {
		//deep, err := w.chain.ReadOfficialNFTPool(w.chain.CurrentHeader())
		//fmt.Println("deep", deep, "err", err)
		w.commit(uncles, nil, false, tstart)
	}
	// Fill the block with all available pending transactions.
	pending, err := w.eth.TxPool().Pending(false)
	if err != nil {
		log.Error("Failed to fetch pending transactions", "err", err)
		return
	}
	// Short circuit if there is no available pending transactions.
	// But if we disable empty precommit already, ignore it. Since
	// empty block is necessary to keep the liveness of the network.
	if len(pending) == 0 && atomic.LoadUint32(&w.noempty) == 0 {
		w.updateSnapshot()
		return
	}

	// Split the pending transactions into locals and remotes
	localTxs, remoteTxs := make(map[common.Address]types.Transactions), pending
	for _, account := range w.eth.TxPool().Locals() {
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}

	if len(localTxs) > 0 {
		log.Info("caver|commitNewWork|localTxs", "no", header.Number, "len", len(localTxs))
		txs := types.NewTransactionsByPriceAndNonce(w.current.signer, localTxs, header.BaseFee)
		if w.commitTransactions(txs, w.coinbase, interrupt) {
			return
		}
	}
	if len(remoteTxs) > 0 {
		log.Info("caver|commitNewWork|remoteTxs", "no", header.Number, "len", len(remoteTxs))
		txs := types.NewTransactionsByPriceAndNonce(w.current.signer, remoteTxs, header.BaseFee)
		if w.commitTransactions(txs, w.coinbase, interrupt) {
			return
		}
	}
	//deep, err := w.chain.ReadMintDeep(w.chain.CurrentHeader())
	//fmt.Println("deep", deep, "err", err)
	w.commit(uncles, w.fullTaskHook, true, tstart)
}

// commit runs any post-transaction state modifications, assembles the final block
// and commits new work if consensus engine is running.
func (w *worker) commit(uncles []*types.Header, interval func(), update bool, start time.Time) error {
	log.Info("caver|commit|enter", "no", w.chain.CurrentHeader().Number.Uint64()+1)
	// Deep copy receipts here
	//to avoid interaction between different tasks.
	receipts := copyReceipts(w.current.receipts)
	s := w.current.state.Copy()
	block, err := w.engine.FinalizeAndAssemble(w.chain, w.current.header, s, w.current.txs, uncles, receipts)
	if err != nil {
		log.Info("caver|commit|w.engine.FinalizeAndAssemble", "no", w.current.header.Number.Uint64(), "err", err.Error())
		return err
	}
	if w.isRunning() {
		if interval != nil {
			interval()
		}
		select {
		case w.taskCh <- &task{receipts: receipts, state: s, block: block, createdAt: time.Now()}:
			w.unconfirmed.Shift(block.NumberU64() - 1)
			log.Info("Commit new mining work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
				"uncles", len(uncles), "txs", w.current.tcount,
				"gas", block.GasUsed(), "fees", totalFees(block, receipts),
				"elapsed", common.PrettyDuration(time.Since(start)))

		case <-w.exitCh:
			log.Info("Worker has exited")
		}
	}
	if update {
		w.updateSnapshot()
	}
	return nil
}

// copyReceipts makes a deep copy of the given receipts.
func copyReceipts(receipts []*types.Receipt) []*types.Receipt {
	result := make([]*types.Receipt, len(receipts))
	for i, l := range receipts {
		cpy := *l
		result[i] = &cpy
	}
	return result
}

// postSideBlock fires a side chain event, only use it for testing.
func (w *worker) postSideBlock(event core.ChainSideEvent) {
	select {
	case w.chainSideCh <- event:
	case <-w.exitCh:
	}
}

// totalFees computes total consumed miner fees in ETH. Block transactions and receipts have to have the same order.
func totalFees(block *types.Block, receipts []*types.Receipt) *big.Float {
	feesWei := new(big.Int)
	for i, tx := range block.Transactions() {
		minerFee, _ := tx.EffectiveGasTip(block.BaseFee())
		feesWei.Add(feesWei, new(big.Int).Mul(new(big.Int).SetUint64(receipts[i].GasUsed), minerFee))
	}
	return new(big.Float).Quo(new(big.Float).SetInt(feesWei), new(big.Float).SetInt(big.NewInt(params.Ether)))
}

func GetBFTSize(len int) int {
	return 2*(int(math.Ceil(float64(len)/3))-1) + 1
}

func (w *worker) targetSize() *big.Int {
	return w.cerytify.stakers.TargetSize()
}

func (w *worker) targetSizeWithWeight() (*big.Int, error) {
	var total = big.NewInt(0)
	currentState, err := w.chain.StateAt(w.chain.CurrentBlock().Root())
	if err != nil {
		return big.NewInt(0), err
	}
	var voteBalance *big.Int
	var coe uint8
	//log.Info("targetSizeWithWeight:w.cerytify.stakers.Validators", "height", w.chain.CurrentBlock().NumberU64()+1, "len", len(w.cerytify.stakers.Validators))
	for _, voter := range w.cerytify.stakers.Validators {
		coe = currentState.GetValidatorCoefficient(voter.Addr)
		voteBalance = new(big.Int).Mul(voter.Balance, big.NewInt(int64(coe)))
		total.Add(total, voteBalance)
		//log.Info("targetSizeWithWeight:info", "height", w.chain.CurrentBlock().NumberU64()+1, "coe", coe, "voter.Balance", voter.Balance, "voteBalance", voteBalance, "total", total)
	}
	a := new(big.Int).Mul(big.NewInt(50), total)
	b := new(big.Int).Div(a, big.NewInt(100))
	return b, nil
}

func (w *worker) getValidatorCoefficient(address common.Address) (uint8, error) {
	currentState, err := w.chain.StateAt(w.chain.CurrentBlock().Root())
	if err != nil {
		return 0, err
	}
	validatorAddress := w.cerytify.stakers.GetValidatorAddr(address)
	//log.Info("worker.getValidatorCoefficient", "address", address.Hex(), "validator address", validatorAddress.Hex())
	coe := currentState.GetValidatorCoefficient(validatorAddress)
	return coe, nil
}

func (w *worker) GetAverageCoefficient() (uint64, error) {
	var total = big.NewInt(0)
	var maxTotal = big.NewInt(0)
	currentState, err := w.chain.StateAt(w.chain.CurrentBlock().Root())
	if err != nil {
		return 0, err
	}
	var voteBalance *big.Int
	var maxVoteBalance *big.Int
	var coe uint8
	//log.Info("GetAverageCoefficient:w.cerytify.stakers.Validators", "height", w.chain.CurrentBlock().NumberU64()+1, "len", len(w.cerytify.stakers.Validators))
	for _, voter := range w.cerytify.stakers.Validators {
		coe = currentState.GetValidatorCoefficient(voter.Addr)
		voteBalance = new(big.Int).Mul(voter.Balance, big.NewInt(int64(coe)))
		total.Add(total, voteBalance)
		maxVoteBalance = new(big.Int).Mul(voter.Balance, big.NewInt(types.DEFAULT_VALIDATOR_COEFFICIENT))
		maxTotal.Add(maxTotal, maxVoteBalance)
		//log.Info("GetAverageCoefficient:info", "height", w.chain.CurrentBlock().NumberU64()+1,
		//	"coe", coe, "voter.Balance", voter.Balance, "voteBalance", voteBalance, "total", total,
		//	"maxVoteBalance", maxVoteBalance, "maxTotal", maxTotal)
	}

	ratio := new(big.Float).Quo(new(big.Float).SetInt(total), new(big.Float).SetInt(maxTotal))
	bigFloatCoefficient := new(big.Float).Mul(ratio, big.NewFloat(types.DEFAULT_VALIDATOR_COEFFICIENT))
	averageCoe, _ := new(big.Float).Mul(bigFloatCoefficient, big.NewFloat(10)).Uint64()
	log.Info("GetAverageCoefficient: average coefficient", "total", total, "maxTotal", maxTotal,
		"ratio", ratio, "bigFloatCoefficient", bigFloatCoefficient, "averageCoe", averageCoe, "height", w.chain.CurrentBlock().NumberU64()+1)
	return averageCoe, nil
}

func (w *worker) getNodeAddr() common.Address {
	return ethcrypto.PubkeyToAddress(w.eth.GetNodeKey().PublicKey)
}

func IntToBytes(n int) []byte {
	data := int32(n)
	bytebuf := bytes.NewBuffer([]byte{})
	binary.Write(bytebuf, binary.BigEndian, data)
	return bytebuf.Bytes()
}

func (w *worker) makeProofCurrent(parent *types.Block, header *types.Header) error {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit
	state, err := w.chain.StateAt(parent.Root())
	if err != nil {
		return err
	}
	state.StartPrefetcher("miner")

	var mintDeep *types.MintDeep
	//var exchangeList *types.SNFTExchangeList
	if parent.NumberU64() > 0 {
		mintDeep, err = w.chain.ReadMintDeep(parent.Header())
		if err != nil {
			log.Error("Failed get mintdeep ", "err", err)
			return err
		}
		//exchangeList, _ = w.chain.ReadSNFTExchangePool(parent.Header())
		//if exchangeList == nil {
		//	exchangeList = &types.SNFTExchangeList{
		//		SNFTExchanges: make([]*types.SNFTExchange, 0),
		//	}
		//}
	} else {
		mintDeep = new(types.MintDeep)
		//mintDeep.OfficialMint = big.NewInt(1)
		//
		//mintDeep.UserMint = big.NewInt(0)
		//maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
		//mintDeep.UserMint.Add(big.NewInt(1), maskB)
		mintDeep.UserMint = big.NewInt(1)

		mintDeep.OfficialMint = big.NewInt(0)
		maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
		mintDeep.OfficialMint.Add(big.NewInt(0), maskB)

		//exchangeList = &types.SNFTExchangeList{
		//	SNFTExchanges: make([]*types.SNFTExchange, 0),
		//}
	}
	state.MintDeep = mintDeep
	//state.SNFTExchangePool = exchangeList

	officialNFTList, _ := w.chain.ReadOfficialNFTPool(parent.Header())
	state.OfficialNFTPool = officialNFTList
	for _, v := range state.OfficialNFTPool.InjectedOfficialNFTs {
		log.Info("makeCurrent()", "state.OfficialNFTPool.InjectedOfficialNFTs", v)
	}

	var nominatedOfficialNFT *types.NominatedOfficialNFT
	if parent.NumberU64() > 0 {
		nominatedOfficialNFT, err = w.chain.ReadNominatedOfficialNFT(parent.Header())
		if err != nil {
			state.NominatedOfficialNFT = nil
		} else {
			state.NominatedOfficialNFT = nominatedOfficialNFT
		}
	} else {
		nominatedOfficialNFT = new(types.NominatedOfficialNFT)
		nominatedOfficialNFT.Dir = types.DefaultDir
		nominatedOfficialNFT.StartIndex = new(big.Int).Set(state.OfficialNFTPool.MaxIndex())
		nominatedOfficialNFT.Number = types.DefaultNumber
		nominatedOfficialNFT.Royalty = types.DefaultRoyalty
		nominatedOfficialNFT.Creator = types.DefaultCreator
		nominatedOfficialNFT.Address = common.Address{}
		state.NominatedOfficialNFT = nominatedOfficialNFT
	}

	vallist, err := w.chain.ReadValidatorPool(parent.Header())
	if err != nil {
		log.Error("makeProofCurrent : invalid validator list", "no", header.Number, "err", err)
		return err
	}
	state.ValidatorPool = vallist.Validators

	env := &environment{
		signer:    types.MakeSigner(w.chainConfig, header.Number),
		state:     state,
		ancestors: mapset.NewSet(),
		family:    mapset.NewSet(),
		uncles:    mapset.NewSet(),
		header:    header,
	}
	// when 08 is processed ancestors contain 07 (quick block)
	for _, ancestor := range w.chain.GetBlocksFromHash(parent.Hash(), 7) {
		for _, uncle := range ancestor.Uncles() {
			env.family.Add(uncle.Hash())
		}
		env.family.Add(ancestor.Hash())
		env.ancestors.Add(ancestor.Hash())
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0

	// Swap out the old work with the new one, terminating any leftover prefetcher
	// processes in the mean time and starting a new one.
	if w.proofcurrent != nil && w.proofcurrent.state != nil {
		w.proofcurrent.state.StopPrefetcher()
	}
	w.proofcurrent = env
	return nil
}
