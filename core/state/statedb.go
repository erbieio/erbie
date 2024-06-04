// Copyright 2014 The go-ethereum Authors
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

// Package state provides a caching layer atop the Ethereum state trie.
package state

import (
	"encoding/hex"
	"errors"
	"fmt"
	gomath "math"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type revision struct {
	id           int
	journalIndex int
}

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
)

type proofList [][]byte

func (n *proofList) Put(key []byte, value []byte) error {
	*n = append(*n, value)
	return nil
}

func (n *proofList) Delete(key []byte) error {
	panic("not supported")
}

// StateDB structs within the ethereum protocol are used to store anything
// within the merkle trie. StateDBs take care of caching and storing
// nested states. It's the general query interface to retrieve:
// * Contracts
// * Accounts
type StateDB struct {
	db           Database
	prefetcher   *triePrefetcher
	originalRoot common.Hash // The pre-state root, before any changes were made
	trie         Trie
	hasher       crypto.KeccakState

	snaps         *snapshot.Tree
	snap          snapshot.Snapshot
	snapDestructs map[common.Hash]struct{}
	snapAccounts  map[common.Hash][]byte
	snapStorage   map[common.Hash]map[common.Hash][]byte

	// This map holds 'live' objects, which will get modified while processing a state transition.
	stateObjects        map[common.Address]*stateObject
	stateObjectsPending map[common.Address]struct{} // State objects finalized but not yet written to the trie
	stateObjectsDirty   map[common.Address]struct{} // State objects modified in the current execution

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error

	// The refund counter, also used by state transitioning.
	refund uint64

	thash   common.Hash
	txIndex int
	logs    map[common.Hash][]*types.Log
	logSize uint

	preimages map[common.Hash][]byte

	// Per-transaction access list
	accessList *accessList

	// Journal of state modifications. This is the backbone of
	// Snapshot and RevertToSnapshot.
	journal        *journal
	validRevisions []revision
	nextRevisionId int

	// Measurements gathered during execution for debugging purposes
	AccountReads         time.Duration
	AccountHashes        time.Duration
	AccountUpdates       time.Duration
	AccountCommits       time.Duration
	StorageReads         time.Duration
	StorageHashes        time.Duration
	StorageUpdates       time.Duration
	StorageCommits       time.Duration
	SnapshotAccountReads time.Duration
	SnapshotStorageReads time.Duration
	SnapshotCommits      time.Duration
}

// New creates a new state from a given trie.
func New(root common.Hash, db Database, snaps *snapshot.Tree) (*StateDB, error) {
	tr, err := db.OpenTrie(root)
	if err != nil {
		return nil, err
	}
	sdb := &StateDB{
		db:                  db,
		trie:                tr,
		originalRoot:        root,
		snaps:               snaps,
		stateObjects:        make(map[common.Address]*stateObject),
		stateObjectsPending: make(map[common.Address]struct{}),
		stateObjectsDirty:   make(map[common.Address]struct{}),
		logs:                make(map[common.Hash][]*types.Log),
		preimages:           make(map[common.Hash][]byte),
		journal:             newJournal(),
		accessList:          newAccessList(),
		hasher:              crypto.NewKeccakState(),
	}
	if sdb.snaps != nil {
		if sdb.snap = sdb.snaps.Snapshot(root); sdb.snap != nil {
			sdb.snapDestructs = make(map[common.Hash]struct{})
			sdb.snapAccounts = make(map[common.Hash][]byte)
			sdb.snapStorage = make(map[common.Hash]map[common.Hash][]byte)
		}
	}
	return sdb, nil
}

// StartPrefetcher initializes a new trie prefetcher to pull in nodes from the
// state trie concurrently while the state is mutated so that when we reach the
// commit phase, most of the needed data is already hot.
func (s *StateDB) StartPrefetcher(namespace string) {
	if s.prefetcher != nil {
		s.prefetcher.close()
		s.prefetcher = nil
	}
	if s.snap != nil {
		s.prefetcher = newTriePrefetcher(s.db, s.originalRoot, namespace)
	}
}

// StopPrefetcher terminates a running prefetcher and reports any leftover stats
// from the gathered metrics.
func (s *StateDB) StopPrefetcher() {
	if s.prefetcher != nil {
		s.prefetcher.close()
		s.prefetcher = nil
	}
}

// setError remembers the first non-nil error it is called with.
func (s *StateDB) setError(err error) {
	if s.dbErr == nil {
		s.dbErr = err
	}
}

func (s *StateDB) Error() error {
	return s.dbErr
}

func (s *StateDB) AddLog(log *types.Log) {
	s.journal.append(addLogChange{txhash: s.thash})

	log.TxHash = s.thash
	log.TxIndex = uint(s.txIndex)
	log.Index = s.logSize
	s.logs[s.thash] = append(s.logs[s.thash], log)
	s.logSize++
}

func (s *StateDB) GetLogs(hash common.Hash, blockHash common.Hash) []*types.Log {
	logs := s.logs[hash]
	for _, l := range logs {
		l.BlockHash = blockHash
	}
	return logs
}

func (s *StateDB) Logs() []*types.Log {
	var logs []*types.Log
	for _, lgs := range s.logs {
		logs = append(logs, lgs...)
	}
	return logs
}

// AddPreimage records a SHA3 preimage seen by the VM.
func (s *StateDB) AddPreimage(hash common.Hash, preimage []byte) {
	if _, ok := s.preimages[hash]; !ok {
		s.journal.append(addPreimageChange{hash: hash})
		pi := make([]byte, len(preimage))
		copy(pi, preimage)
		s.preimages[hash] = pi
	}
}

// Preimages returns a list of SHA3 preimages that have been submitted.
func (s *StateDB) Preimages() map[common.Hash][]byte {
	return s.preimages
}

// AddRefund adds gas to the refund counter
func (s *StateDB) AddRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *StateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	if gas > s.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.refund))
	}
	s.refund -= gas
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (s *StateDB) Exist(addr common.Address) bool {
	return s.getStateObject(addr) != nil
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (s *StateDB) Empty(addr common.Address) bool {
	so := s.getStateObject(addr)
	return so == nil || so.empty()
}

// GetBalance retrieves the balance from the given address or 0 if object not found
func (s *StateDB) GetBalance(addr common.Address) *big.Int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Balance()
	}
	return common.Big0
}

func (s *StateDB) GetNonce(addr common.Address) uint64 {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Nonce()
	}

	return 0
}

// TxIndex returns the current transaction index set by Prepare.
func (s *StateDB) TxIndex() int {
	return s.txIndex
}

func (s *StateDB) GetCode(addr common.Address) []byte {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Code(s.db)
	}
	return nil
}

func (s *StateDB) GetCodeSize(addr common.Address) int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.CodeSize(s.db)
	}
	return 0
}

func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return common.Hash{}
	}
	return common.BytesToHash(stateObject.CodeHash())
}

// GetState retrieves a value from the given account's storage trie.
func (s *StateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetState(s.db, hash)
	}
	return common.Hash{}
}

// GetProof returns the Merkle proof for a given account.
func (s *StateDB) GetProof(addr common.Address) ([][]byte, error) {
	return s.GetProofByHash(crypto.Keccak256Hash(addr.Bytes()))
}

// GetProofByHash returns the Merkle proof for a given account.
func (s *StateDB) GetProofByHash(addrHash common.Hash) ([][]byte, error) {
	var proof proofList
	err := s.trie.Prove(addrHash[:], 0, &proof)
	return proof, err
}

// GetStorageProof returns the Merkle proof for given storage slot.
func (s *StateDB) GetStorageProof(a common.Address, key common.Hash) ([][]byte, error) {
	var proof proofList
	trie := s.StorageTrie(a)
	if trie == nil {
		return proof, errors.New("storage trie for requested address does not exist")
	}
	err := trie.Prove(crypto.Keccak256(key.Bytes()), 0, &proof)
	return proof, err
}

// GetCommittedState retrieves a value from the given account's committed storage trie.
func (s *StateDB) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetCommittedState(s.db, hash)
	}
	return common.Hash{}
}

// Database retrieves the low level database supporting the lower level trie ops.
func (s *StateDB) Database() Database {
	return s.db
}

// StorageTrie returns the storage trie of an account.
// The return value is a copy and is nil for non-existent accounts.
func (s *StateDB) StorageTrie(addr common.Address) Trie {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return nil
	}
	cpy := stateObject.deepCopy(s)
	cpy.updateTrie(s.db)
	return cpy.getTrie(s.db)
}

func (s *StateDB) HasSuicided(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.suicided
	}
	return false
}

/*
 * SETTERS
 */

// AddBalance adds amount to the account associated with addr.
func (s *StateDB) AddBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.AddBalance(amount)
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (s *StateDB) SubBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SubBalance(amount)
	}
}

func (s *StateDB) SetBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SetBalance(amount)
	}
}

func (s *StateDB) SetNonce(addr common.Address, nonce uint64) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SetNonce(nonce)
	}
}

func (s *StateDB) SetCode(addr common.Address, code []byte) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SetCode(crypto.Keccak256Hash(code), code)
	}
}

func (s *StateDB) SetState(addr common.Address, key, value common.Hash) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SetState(s.db, key, value)
	}
}

// SetStorage replaces the entire storage for the specified account with given
// storage. This function should only be used for debugging.
func (s *StateDB) SetStorage(addr common.Address, storage map[common.Hash]common.Hash) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SetStorage(storage)
	}
}

// Suicide marks the given account as suicided.
// This clears the account balance.
//
// The account's state object is still available until the state is committed,
// getStateObject will return a non-nil account after Suicide.
func (s *StateDB) Suicide(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return false
	}
	s.journal.append(suicideChange{
		account:     &addr,
		prev:        stateObject.suicided,
		prevbalance: new(big.Int).Set(stateObject.Balance()),
	})
	stateObject.markSuicided()
	stateObject.data.Balance = new(big.Int)

	return true
}

//
// Setting, updating & deleting state object methods.
//

// updateStateObject writes the given object to the trie.
func (s *StateDB) updateStateObject(obj *stateObject) {
	// Track the amount of time wasted on updating the account from the trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountUpdates += time.Since(start) }(time.Now())
	}
	// Encode the account and update the account trie
	addr := obj.Address()

	data, err := rlp.EncodeToBytes(obj)

	var tempObj stateObject
	var acc Account
	rlp.DecodeBytes(data, &tempObj)
	rlp.DecodeBytes(data, &acc)

	if err != nil {
		panic(fmt.Errorf("can't encode object at %x: %v", addr[:], err))
	}
	if err = s.trie.TryUpdate(addr[:], data); err != nil {
		s.setError(fmt.Errorf("updateStateObject (%x) error: %v", addr[:], err))
	}

	// If state snapshotting is active, cache the data til commit. Note, this
	// update mechanism is not symmetric to the deletion, because whereas it is
	// enough to track account updates at commit time, deletions need tracking
	// at transaction boundary level to ensure we capture state clearing.
	if s.snap != nil {
		s.snapAccounts[obj.addrHash] = snapshot.SlimAccountRLP(obj.data.Nonce,
			obj.data.Balance,
			obj.data.Root,
			obj.data.CodeHash,
			obj.data.Worm,
			obj.data.Csbt,
			obj.data.Staker,
			obj.data.Extra)
		//s.snapAccounts[obj.addrHash] = snapshot.SlimAccountRLP(obj.data.Nonce, obj.data.Balance, obj.data.Root, obj.data.CodeHash)
	}
}

// deleteStateObject removes the given object from the state trie.
func (s *StateDB) deleteStateObject(obj *stateObject) {
	// Track the amount of time wasted on deleting the account from the trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountUpdates += time.Since(start) }(time.Now())
	}
	// Delete the account from the trie
	addr := obj.Address()
	if err := s.trie.TryDelete(addr[:]); err != nil {
		s.setError(fmt.Errorf("deleteStateObject (%x) error: %v", addr[:], err))
	}
}

// getStateObject retrieves a state object given by the address, returning nil if
// the object is not found or was deleted in this execution context. If you need
// to differentiate between non-existent/just-deleted, use getDeletedStateObject.
func (s *StateDB) getStateObject(addr common.Address) *stateObject {
	if obj := s.getDeletedStateObject(addr); obj != nil && !obj.deleted {
		return obj
	}
	return nil
}

// getDeletedStateObject is similar to getStateObject, but instead of returning
// nil for a deleted state object, it returns the actual object with the deleted
// flag set. This is needed by the state journal to revert to the correct s-
// destructed object instead of wiping all knowledge about the state object.
func (s *StateDB) getDeletedStateObject(addr common.Address) *stateObject {
	// Prefer live objects if any is available
	if obj := s.stateObjects[addr]; obj != nil {
		return obj
	}
	// If no live objects are available, attempt to use snapshots
	var (
		data *Account
		err  error
	)
	if s.snap != nil {
		if metrics.EnabledExpensive {
			defer func(start time.Time) { s.SnapshotAccountReads += time.Since(start) }(time.Now())
		}
		var acc *snapshot.Account
		if acc, err = s.snap.Account(crypto.HashData(s.hasher, addr.Bytes())); err == nil {
			if acc == nil {
				return nil
			}
			data = &Account{
				Nonce:    acc.Nonce,
				Balance:  acc.Balance,
				CodeHash: acc.CodeHash,
				Root:     common.BytesToHash(acc.Root),
				Worm:     acc.Worm,
				Csbt:     acc.Csbt,
				Staker:   acc.Staker,
				Extra:    acc.Extra,
			}
			if len(data.CodeHash) == 0 {
				data.CodeHash = emptyCodeHash
			}
			if data.Root == (common.Hash{}) {
				data.Root = emptyRoot
			}
		}
	}
	// If snapshot unavailable or reading from it failed, load from the database
	if s.snap == nil || err != nil {
		if metrics.EnabledExpensive {
			defer func(start time.Time) { s.AccountReads += time.Since(start) }(time.Now())
		}
		enc, err := s.trie.TryGet(addr.Bytes())
		if err != nil {
			s.setError(fmt.Errorf("getDeleteStateObject (%x) error: %v", addr.Bytes(), err))
			return nil
		}
		if len(enc) == 0 {
			return nil
		}
		data = new(Account)
		if err := rlp.DecodeBytes(enc, data); err != nil {
			log.Error("Failed to decode state object", "addr", addr, "err", err)
			return nil
		}
	}
	// Insert into the live set
	obj := newObject(s, addr, *data)
	s.setStateObject(obj)
	return obj
}

// for test
func (s *StateDB) getDeletedStateObject2(addr common.Address) *stateObject {
	// Prefer live objects if any is available
	//if obj := s.stateObjects[addr]; obj != nil {
	//	return obj
	//}
	// If no live objects are available, attempt to use snapshots
	var (
		data *Account
		err  error
	)
	//if s.snap != nil {
	//	if metrics.EnabledExpensive {
	//		defer func(start time.Time) { s.SnapshotAccountReads += time.Since(start) }(time.Now())
	//	}
	//	var acc *snapshot.Account
	//	if acc, err = s.snap.Account(crypto.HashData(s.hasher, addr.Bytes())); err == nil {
	//		if acc == nil {
	//			return nil
	//		}
	//		data = &Account{
	//			Nonce:    acc.Nonce,
	//			Balance:  acc.Balance,
	//			CodeHash: acc.CodeHash,
	//			Root:     common.BytesToHash(acc.Root),
	//			// *** modify to support nft transaction 20211217 begin ***
	//			Owner: acc.Owner,
	//			// *** modify to support nft transaction 20211217 end ***
	//		}
	//		if len(data.CodeHash) == 0 {
	//			data.CodeHash = emptyCodeHash
	//		}
	//		if data.Root == (common.Hash{}) {
	//			data.Root = emptyRoot
	//		}
	//	}
	//}
	// If snapshot unavailable or reading from it failed, load from the database
	//if s.snap == nil || err != nil {
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountReads += time.Since(start) }(time.Now())
	}
	enc, err := s.trie.TryGet(addr.Bytes())
	if err != nil {
		s.setError(fmt.Errorf("getDeleteStateObject (%x) error: %v", addr.Bytes(), err))
		return nil
	}
	if len(enc) == 0 {
		return nil
	}
	data = new(Account)
	if err := rlp.DecodeBytes(enc, data); err != nil {
		log.Error("Failed to decode state object", "addr", addr, "err", err)
		return nil
	}
	//}
	// Insert into the live set
	obj := newObject(s, addr, *data)
	s.setStateObject(obj)
	return obj
}

func (s *StateDB) setStateObject(object *stateObject) {
	s.stateObjects[object.Address()] = object
}

// GetOrNewStateObject retrieves a state object or create a new state object if nil.
func (s *StateDB) GetOrNewStateObject(addr common.Address) *stateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject, _ = s.createObject(addr)
	}

	return stateObject
}

func (s *StateDB) GetOrNewNFTStateObject(addr common.Address) *stateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject, _ = s.createObject(addr)
		stateObject.data.Worm = nil
		stateObject.data.Csbt = &types.AccountCSBT{}
	}

	if stateObject.data.Csbt == nil {
		stateObject.data.Csbt = &types.AccountCSBT{}
	}

	return stateObject
}

func (s *StateDB) GetOrNewAccountStateObject(addr common.Address) *stateObject {
	var prev *stateObject
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject, prev = s.createObject(addr)
		if prev != nil {
			stateObject.setBalance(prev.data.Balance)
		}
		stateObject.data.Worm = &types.WormholesExtension{}
		stateObject.data.Csbt = nil
	}

	if stateObject.data.Worm == nil {
		stateObject.data.Worm = &types.WormholesExtension{}
	}

	if stateObject.data.Worm.PledgedBlockNumber == nil {
		stateObject.data.Worm.PledgedBlockNumber = big.NewInt(0)
	}
	if stateObject.data.Worm.PledgedBalance == nil {
		stateObject.data.Worm.PledgedBalance = big.NewInt(0)
	}

	return stateObject
}

func (s *StateDB) GetOrNewStakerStateObject(addr common.Address) *stateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject, _ = s.createObject(addr)
		stateObject.data.Staker = &types.AccountStaker{}
		if addr == types.MintDeepStorageAddress {
			stateObject.data.Staker.Mint.UserMint = big.NewInt(1)
			maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
			stateObject.data.Staker.Mint.OfficialMint = maskB
		}
	}

	if stateObject.data.Staker == nil {
		stateObject.data.Staker = &types.AccountStaker{}
		if addr == types.MintDeepStorageAddress {
			stateObject.data.Staker.Mint.UserMint = big.NewInt(1)
			maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
			stateObject.data.Staker.Mint.OfficialMint = maskB
		}
	}

	return stateObject
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
func (s *StateDB) createObject(addr common.Address) (newobj, prev *stateObject) {
	prev = s.getDeletedStateObject(addr) // Note, prev might have been deleted, we need that!

	var prevdestruct bool
	if s.snap != nil && prev != nil {
		_, prevdestruct = s.snapDestructs[prev.addrHash]
		if !prevdestruct {
			s.snapDestructs[prev.addrHash] = struct{}{}
		}
	}
	//newobj = newObject(s, addr, Account{RewardFlag: 1})
	newobj = newObject(s, addr, Account{})
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev, prevdestruct: prevdestruct})
	}
	s.setStateObject(newobj)
	if prev != nil && !prev.deleted {
		return newobj, prev
	}
	return newobj, nil
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
//  1. sends funds to sha(account ++ (nonce + 1))
//  2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (s *StateDB) CreateAccount(addr common.Address) {
	//newObj, prev := s.createObject(addr)
	//if prev != nil {
	//	newObj.setBalance(prev.data.Balance)
	//}
	//if newObj.data.Worm == nil {
	//	newObj.data.Worm = &types.WormholesExtension{}
	//}
	s.GetOrNewAccountStateObject(addr)
}

func (s *StateDB) CreateNFTAccount(addr common.Address) {
	//newObj, prev := s.createObject(addr)
	//if prev != nil {
	//	newObj.setBalance(prev.data.Balance)
	//}
	//if newObj.data.Nft == nil {
	//	newObj.data.Nft = &types.AccountNFT{}
	//}
	s.GetOrNewNFTStateObject(addr)
}

func (s *StateDB) CreateStakerAccount(addr common.Address) {
	//newObj, prev := s.createObject(addr)
	//if prev != nil {
	//	newObj.setBalance(prev.data.Balance)
	//}
	//newObj.data.Staker = &types.AccountStaker{}
	//if addr == types.MintDeepStorageAddress {
	//	newObj.data.Staker.Mint.UserMint = big.NewInt(1)
	//	maskB, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
	//	newObj.data.Staker.Mint.OfficialMint = maskB
	//}
	s.GetOrNewStakerStateObject(addr)
}

func (db *StateDB) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	so := db.getStateObject(addr)
	if so == nil {
		return nil
	}
	it := trie.NewIterator(so.getTrie(db.db).NodeIterator(nil))

	for it.Next() {
		key := common.BytesToHash(db.trie.GetKey(it.Key))
		if value, dirty := so.dirtyStorage[key]; dirty {
			if !cb(key, value) {
				return nil
			}
			continue
		}

		if len(it.Value) > 0 {
			_, content, _, err := rlp.Split(it.Value)
			if err != nil {
				return err
			}
			if !cb(key, common.BytesToHash(content)) {
				return nil
			}
		}
	}
	return nil
}

// Copy creates a deep, independent copy of the state.
// Snapshots of the copied state cannot be applied to the copy.
func (s *StateDB) Copy() *StateDB {
	// Copy all the basic fields, initialize the memory ones
	state := &StateDB{
		db:                  s.db,
		trie:                s.db.CopyTrie(s.trie),
		stateObjects:        make(map[common.Address]*stateObject, len(s.journal.dirties)),
		stateObjectsPending: make(map[common.Address]struct{}, len(s.stateObjectsPending)),
		stateObjectsDirty:   make(map[common.Address]struct{}, len(s.journal.dirties)),
		refund:              s.refund,
		logs:                make(map[common.Hash][]*types.Log, len(s.logs)),
		logSize:             s.logSize,
		preimages:           make(map[common.Hash][]byte, len(s.preimages)),
		journal:             newJournal(),
		hasher:              crypto.NewKeccakState(),
	}
	// Copy the dirty states, logs, and preimages
	for addr := range s.journal.dirties {
		// As documented [here](https://github.com/ethereum/go-ethereum/pull/16485#issuecomment-380438527),
		// and in the Finalise-method, there is a case where an object is in the journal but not
		// in the stateObjects: OOG after touch on ripeMD prior to Byzantium. Thus, we need to check for
		// nil
		if object, exist := s.stateObjects[addr]; exist {
			// Even though the original object is dirty, we are not copying the journal,
			// so we need to make sure that anyside effect the journal would have caused
			// during a commit (or similar op) is already applied to the copy.
			state.stateObjects[addr] = object.deepCopy(state)

			state.stateObjectsDirty[addr] = struct{}{}   // Mark the copy dirty to force internal (code/state) commits
			state.stateObjectsPending[addr] = struct{}{} // Mark the copy pending to force external (account) commits
		}
	}
	// Above, we don't copy the actual journal. This means that if the copy is copied, the
	// loop above will be a no-op, since the copy's journal is empty.
	// Thus, here we iterate over stateObjects, to enable copies of copies
	for addr := range s.stateObjectsPending {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = s.stateObjects[addr].deepCopy(state)
		}
		state.stateObjectsPending[addr] = struct{}{}
	}
	for addr := range s.stateObjectsDirty {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = s.stateObjects[addr].deepCopy(state)
		}
		state.stateObjectsDirty[addr] = struct{}{}
	}
	for hash, logs := range s.logs {
		cpy := make([]*types.Log, len(logs))
		for i, l := range logs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		state.logs[hash] = cpy
	}
	for hash, preimage := range s.preimages {
		state.preimages[hash] = preimage
	}
	// Do we need to copy the access list? In practice: No. At the start of a
	// transaction, the access list is empty. In practice, we only ever copy state
	// _between_ transactions/blocks, never in the middle of a transaction.
	// However, it doesn't cost us much to copy an empty list, so we do it anyway
	// to not blow up if we ever decide copy it in the middle of a transaction
	state.accessList = s.accessList.Copy()

	// If there's a prefetcher running, make an inactive copy of it that can
	// only access data but does not actively preload (since the user will not
	// know that they need to explicitly terminate an active copy).
	if s.prefetcher != nil {
		state.prefetcher = s.prefetcher.copy()
	}
	if s.snaps != nil {
		// In order for the miner to be able to use and make additions
		// to the snapshot tree, we need to copy that aswell.
		// Otherwise, any block mined by ourselves will cause gaps in the tree,
		// and force the miner to operate trie-backed only
		state.snaps = s.snaps
		state.snap = s.snap
		// deep copy needed
		state.snapDestructs = make(map[common.Hash]struct{})
		for k, v := range s.snapDestructs {
			state.snapDestructs[k] = v
		}
		state.snapAccounts = make(map[common.Hash][]byte)
		for k, v := range s.snapAccounts {
			state.snapAccounts[k] = v
		}
		state.snapStorage = make(map[common.Hash]map[common.Hash][]byte)
		for k, v := range s.snapStorage {
			temp := make(map[common.Hash][]byte)
			for kk, vv := range v {
				temp[kk] = vv
			}
			state.snapStorage[k] = temp
		}
	}

	return state
}

// Snapshot returns an identifier for the current revision of the state.
func (s *StateDB) Snapshot() int {
	id := s.nextRevisionId
	s.nextRevisionId++
	s.validRevisions = append(s.validRevisions, revision{id, s.journal.length()})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (s *StateDB) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(s.validRevisions), func(i int) bool {
		return s.validRevisions[i].id >= revid
	})
	if idx == len(s.validRevisions) || s.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := s.validRevisions[idx].journalIndex

	// Replay the journal to undo changes and remove invalidated snapshots
	s.journal.revert(s, snapshot)
	s.validRevisions = s.validRevisions[:idx]
}

// GetRefund returns the current value of the refund counter.
func (s *StateDB) GetRefund() uint64 {
	return s.refund
}

// Finalise finalises the state by removing the s destructed objects and clears
// the journal as well as the refunds. Finalise, however, will not push any updates
// into the tries just yet. Only IntermediateRoot or Commit will do that.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	addressesToPrefetch := make([][]byte, 0, len(s.journal.dirties))
	for addr := range s.journal.dirties {
		obj, exist := s.stateObjects[addr]
		if !exist {
			// ripeMD is 'touched' at block 1714175, in tx 0x1237f737031e40bcde4a8b7e717b2d15e3ecadfe49bb1bbc71ee9deb09c6fcf2
			// That tx goes out of gas, and although the notion of 'touched' does not exist there, the
			// touch-event will still be recorded in the journal. Since ripeMD is a special snowflake,
			// it will persist in the journal even though the journal is reverted. In this special circumstance,
			// it may exist in `s.journal.dirties` but not in `s.stateObjects`.
			// Thus, we can safely ignore it here
			continue
		}
		if obj.suicided || (deleteEmptyObjects && obj.empty()) {
			obj.deleted = true

			// If state snapshotting is active, also mark the destruction there.
			// Note, we can't do this only at the end of a block because multiple
			// transactions within the same block might self destruct and then
			// ressurrect an account; but the snapshotter needs both events.
			if s.snap != nil {
				s.snapDestructs[obj.addrHash] = struct{}{} // We need to maintain account deletions explicitly (will remain set indefinitely)
				delete(s.snapAccounts, obj.addrHash)       // Clear out any previously updated account data (may be recreated via a ressurrect)
				delete(s.snapStorage, obj.addrHash)        // Clear out any previously updated storage data (may be recreated via a ressurrect)
			}
		} else {
			obj.finalise(true) // Prefetch slots in the background
		}
		s.stateObjectsPending[addr] = struct{}{}
		s.stateObjectsDirty[addr] = struct{}{}

		// At this point, also ship the address off to the precacher. The precacher
		// will start loading tries, and when the change is eventually committed,
		// the commit-phase will be a lot faster
		addressesToPrefetch = append(addressesToPrefetch, common.CopyBytes(addr[:])) // Copy needed for closure
	}
	if s.prefetcher != nil && len(addressesToPrefetch) > 0 {
		s.prefetcher.prefetch(s.originalRoot, addressesToPrefetch)
	}
	// Invalidate journal because reverting across transactions is not allowed.
	s.clearJournalAndRefund()
}

// IntermediateRoot computes the current root hash of the state trie.
// It is called in between transactions to get the root hash that
// goes into transaction receipts.
func (s *StateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	// Finalise all the dirty storage states and write them into the tries
	//log.Info("caver|IntermediateRoot|enter=0", "triehash", s.trie.Hash().String())
	s.Finalise(deleteEmptyObjects)
	//log.Info("caver|IntermediateRoot|enter=1", "triehash", s.trie.Hash().String())

	// If there was a trie prefetcher operating, it gets aborted and irrevocably
	// modified after we start retrieving tries. Remove it from the statedb after
	// this round of use.
	//
	// This is weird pre-byzantium since the first tx runs with a prefetcher and
	// the remainder without, but pre-byzantium even the initial prefetcher is
	// useless, so no sleep lost.
	prefetcher := s.prefetcher
	if s.prefetcher != nil {
		defer func() {
			s.prefetcher.close()
			s.prefetcher = nil
		}()
	}
	// Although naively it makes sense to retrieve the account trie and then do
	// the contract storage and account updates sequentially, that short circuits
	// the account prefetcher. Instead, let's process all the storage updates
	// first, giving the account prefeches just a few more milliseconds of time
	// to pull useful data from disk.
	for addr := range s.stateObjectsPending {
		if obj := s.stateObjects[addr]; !obj.deleted {
			obj.updateRoot(s.db)
		}
	}
	// Now we're about to start to write changes to the trie. The trie is so far
	// _untouched_. We can check with the prefetcher, if it can give us a trie
	// which has the same root, but also has some content loaded into it.
	if prefetcher != nil {
		if trie := prefetcher.trie(s.originalRoot); trie != nil {
			s.trie = trie
		}
	}
	usedAddrs := make([][]byte, 0, len(s.stateObjectsPending))
	for addr := range s.stateObjectsPending {
		if obj := s.stateObjects[addr]; obj.deleted {
			s.deleteStateObject(obj)
			//log.Info("caver|IntermediateRoot|deleteStateObject", "addr",addr.String(),"triehash", s.trie.Hash().String(), "benefiaddr", obj.data.Owner.Hex())
		} else {
			s.updateStateObject(obj)
			//log.Info("caver|IntermediateRoot|updateStateObject", "addr",addr.String(),"triehash", s.trie.Hash().String(), "benefiaddr", obj.data.Owner.Hex())
		}
		usedAddrs = append(usedAddrs, common.CopyBytes(addr[:])) // Copy needed for closure
	}
	if prefetcher != nil {
		prefetcher.used(s.originalRoot, usedAddrs)
	}
	if len(s.stateObjectsPending) > 0 {
		s.stateObjectsPending = make(map[common.Address]struct{})
	}
	// Track the amount of time wasted on hashing the account trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountHashes += time.Since(start) }(time.Now())
	}
	//log.Info("caver|IntermediateRoot|enter=3", "triehash", s.trie.Hash().String())
	return s.trie.Hash()
}

// Prepare sets the current transaction hash and index which are
// used when the EVM emits new state logs.
func (s *StateDB) Prepare(thash common.Hash, ti int) {
	s.thash = thash
	s.txIndex = ti
	s.accessList = newAccessList()
}

func (s *StateDB) clearJournalAndRefund() {
	if len(s.journal.entries) > 0 {
		s.journal = newJournal()
		s.refund = 0
	}
	s.validRevisions = s.validRevisions[:0] // Snapshots can be created without journal entires
}

// Commit writes the state to the underlying in-memory trie database.
func (s *StateDB) Commit(deleteEmptyObjects bool) (common.Hash, error) {
	if s.dbErr != nil {
		return common.Hash{}, fmt.Errorf("commit aborted due to earlier error: %v", s.dbErr)
	}
	// Finalize any pending changes and merge everything into the tries
	s.IntermediateRoot(deleteEmptyObjects)

	// Commit objects to the trie, measuring the elapsed time
	codeWriter := s.db.TrieDB().DiskDB().NewBatch()
	for addr := range s.stateObjectsDirty {
		if obj := s.stateObjects[addr]; !obj.deleted {
			// Write any contract code associated with the state object
			if obj.code != nil && obj.dirtyCode {
				rawdb.WriteCode(codeWriter, common.BytesToHash(obj.CodeHash()), obj.code)
				obj.dirtyCode = false
			}
			// Write any storage changes in the state object to its storage trie
			if err := obj.CommitTrie(s.db); err != nil {
				return common.Hash{}, err
			}
		}
	}
	if len(s.stateObjectsDirty) > 0 {
		s.stateObjectsDirty = make(map[common.Address]struct{})
	}
	if codeWriter.ValueSize() > 0 {
		if err := codeWriter.Write(); err != nil {
			log.Crit("Failed to commit dirty codes", "error", err)
		}
	}
	// Write the account trie changes, measuing the amount of wasted time
	var start time.Time
	if metrics.EnabledExpensive {
		start = time.Now()
	}
	// The onleaf func is called _serially_, so we can reuse the same account
	// for unmarshalling every time.
	var account Account
	root, err := s.trie.Commit(func(_ [][]byte, _ []byte, leaf []byte, parent common.Hash) error {
		if err := rlp.DecodeBytes(leaf, &account); err != nil {
			return nil
		}
		if account.Root != emptyRoot {
			s.db.TrieDB().Reference(account.Root, parent)
		}
		return nil
	})
	if metrics.EnabledExpensive {
		s.AccountCommits += time.Since(start)
	}
	// If snapshotting is enabled, update the snapshot tree with this new version
	if s.snap != nil {
		if metrics.EnabledExpensive {
			defer func(start time.Time) { s.SnapshotCommits += time.Since(start) }(time.Now())
		}
		// Only update if there's a state transition (skip empty Clique blocks)
		if parent := s.snap.Root(); parent != root {
			if err := s.snaps.Update(root, parent, s.snapDestructs, s.snapAccounts, s.snapStorage); err != nil {
				log.Warn("Failed to update snapshot tree", "from", parent, "to", root, "err", err)
			}
			// Keep 128 diff layers in the memory, persistent layer is 129th.
			// - head layer is paired with HEAD state
			// - head-1 layer is paired with HEAD-1 state
			// - head-127 layer(bottom-most diff layer) is paired with HEAD-127 state
			if err := s.snaps.Cap(root, 128); err != nil {
				log.Warn("Failed to cap snapshot tree", "root", root, "layers", 128, "err", err)
			}
		}
		s.snap, s.snapDestructs, s.snapAccounts, s.snapStorage = nil, nil, nil, nil
	}
	return root, err
}

// PrepareAccessList handles the preparatory steps for executing a state transition with
// regards to both EIP-2929 and EIP-2930:
//
// - Add sender to access list (2929)
// - Add destination to access list (2929)
// - Add precompiles to access list (2929)
// - Add the contents of the optional tx access list (2930)
//
// This method should only be called if Berlin/2929+2930 is applicable at the current number.
func (s *StateDB) PrepareAccessList(sender common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	s.AddAddressToAccessList(sender)
	if dst != nil {
		s.AddAddressToAccessList(*dst)
		// If it's a create-tx, the destination will be added inside evm.create
	}
	for _, addr := range precompiles {
		s.AddAddressToAccessList(addr)
	}
	for _, el := range list {
		s.AddAddressToAccessList(el.Address)
		for _, key := range el.StorageKeys {
			s.AddSlotToAccessList(el.Address, key)
		}
	}
}

// AddAddressToAccessList adds the given address to the access list
func (s *StateDB) AddAddressToAccessList(addr common.Address) {
	if s.accessList.AddAddress(addr) {
		s.journal.append(accessListAddAccountChange{&addr})
	}
}

// AddSlotToAccessList adds the given (address, slot)-tuple to the access list
func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	addrMod, slotMod := s.accessList.AddSlot(addr, slot)
	if addrMod {
		// In practice, this should not happen, since there is no way to enter the
		// scope of 'address' without having the 'address' become already added
		// to the access list (via call-variant, create, etc).
		// Better safe than sorry, though
		s.journal.append(accessListAddAccountChange{&addr})
	}
	if slotMod {
		s.journal.append(accessListAddSlotChange{
			address: &addr,
			slot:    &slot,
		})
	}
}

// AddressInAccessList returns true if the given address is in the access list.
func (s *StateDB) AddressInAccessList(addr common.Address) bool {
	return s.accessList.ContainsAddress(addr)
}

// SlotInAccessList returns true if the given (address, slot)-tuple is in the access list.
func (s *StateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressPresent bool, slotPresent bool) {
	return s.accessList.Contains(addr, slot)
}

// *** modify to support nft transaction 20211215 begin ***

// ChangeNFTOwner change nft's owner to newOwner.
//func (s *StateDB) ChangeNFTOwner(nftAddr common.Address, newOwner common.Address) {
//	stateObject := s.GetOrNewNFTStateObject(nftAddr)
//	if stateObject != nil {
//		s.SplitNFT(nftAddr, 0)
//		stateObject.ChangeNFTOwner(newOwner)
//		// merge nft automatically
//		s.MergeNFT(nftAddr)
//	}
//}

// GetNFTOwner retrieves the nft owner from the given nft address
func (s *StateDB) GetNFTOwner(nftAddr common.Address) common.Address {

	stateObject := s.GetOrNewNFTStateObject(nftAddr)
	if stateObject != nil {
		return stateObject.NFTOwner()
	}

	return common.Address{}
}

// *** modify to support nft transaction 20211215 end ***

func (s *StateDB) ConstructLog(mergedNFTAddress common.Address,
	owner common.Address,
	mergedNFTLevel uint8,
	mergedNFTNumber uint32,
	blockNumber *big.Int,
	mergedNFTs []*MergedNFT) *types.Log {
	var temp string = ""
	//struct SubNFT {
	//	address nft;
	//	uint256 num;
	//}
	//event MergeSNFT(address indexed snft,address indexed owner,uint256 pieces, SubNFT[] subNFTs)
	//event hash: MergeSNFT(address indexed snft,address indexed owner,uint256 pieces, SubNFT[] subNFTs)
	//0x77415a68a0d28daf11e1308e53371f573e0920810c9cd9de7904777d5fb9d625
	hash1 := common.HexToHash("0x77415a68a0d28daf11e1308e53371f573e0920810c9cd9de7904777d5fb9d625")
	nftAddrString := mergedNFTAddress.Hex()
	nftAddrString = string([]byte(nftAddrString)[2 : len(nftAddrString)-int(mergedNFTLevel)])
	for i := 0; i < 64-len(nftAddrString); i++ {
		temp = temp + "0"
	}
	hash2 := common.HexToHash(temp + nftAddrString)
	ownerString := owner.Hex()
	ownerString = string([]byte(ownerString)[2:])
	hash3 := common.HexToHash("000000000000000000000000" + ownerString)

	log := &types.Log{
		Address: common.Address{},
		Topics: []common.Hash{
			hash1,
			hash2,
			hash3,
		},
		Data:        big.NewInt(int64(mergedNFTNumber)).FillBytes(make([]byte, 32)),
		BlockNumber: blockNumber.Uint64(),
	}

	snftNum := len(mergedNFTs)
	if snftNum > 0 {
		temp, _ := hex.DecodeString("0000000000000000000000000000000000000000000000000000000000000080")
		log.Data = append(log.Data, temp...)

		//sub snft num
		log.Data = append(log.Data, big.NewInt(int64(snftNum)).FillBytes(make([]byte, 32))...)

		temp, _ = hex.DecodeString("000000000000000000000000")
		for _, snft := range mergedNFTs {
			log.Data = append(log.Data, temp...)
			log.Data = append(log.Data, snft.Address.Bytes()...)
			log.Data = append(log.Data, big.NewInt(int64(snft.Number)).FillBytes(make([]byte, 32))...)
		}
	}

	return log
}

// ChangeNFTOwner change nft's owner to newOwner.
func (s *StateDB) ChangeNFTOwner(nftAddr common.Address,
	newOwner common.Address,
	level int,
	blocknumber *big.Int) {
	stateObject := s.GetOrNewNFTStateObject(nftAddr)
	if stateObject != nil {
		stateObject.ChangeNFTOwner(newOwner)
	}
}

// if csbts have been merged, original csbts are not exist, they become a new merged csbt
func (s *StateDB) GetNFTOwner16(nftAddr common.Address) common.Address {
	stateObject := s.GetOrNewNFTStateObject(nftAddr)
	if stateObject != nil {
		return stateObject.NFTOwner()
	}

	return common.Address{}
}

func (s *StateDB) IsBeyondOfficialMint(parentAddr string) bool {
	var strF string
	for i := common.AddressLength*2 - len(parentAddr); i > 0; i-- {
		strF = strF + "F"
	}
	parentAddr = parentAddr + strF
	addrInt := big.NewInt(0)
	addrInt.SetString(parentAddr, 16)
	if s.GetOfficialMint().Cmp(addrInt) < 0 {
		return true
	}

	return false
}

type MergedNFT struct {
	Address common.Address `json:"address"`
	Number  uint32         `json:"number"`
}

// Get the store address for a nft
const QUERYDEPTHLIMIT16 = 3

// IsOfficialNFT return true if nft address is created by official
func (s *StateDB) IsOfficialNFT(nftAddress common.Address) bool {
	maskByte := byte(128)
	nftByte := nftAddress[0]
	result := maskByte & nftByte
	if result == 128 {
		return true
	}
	return false
}

func GetRewardAmount(blocknumber uint64, initamount *big.Int) *big.Int {
	times := blocknumber / types.ReduceRewardPeriod
	rewardratio := gomath.Pow(types.DeflationRate, float64(times))
	u, _ := new(big.Float).Mul(big.NewFloat(rewardratio), new(big.Float).SetInt(initamount)).Uint64()

	return new(big.Int).SetUint64(u)
}

func (s *StateDB) CreateNFTByOfficial16(validators, exchangers []common.Address, blocknumber *big.Int, hash []byte) {
	// reward ERB or SNFT to validators
	log.Info("CreateNFTByOfficial16", "validators len=", len(validators), "blocknumber=", blocknumber.Uint64())
	for _, addr := range validators {
		log.Info("CreateNFTByOfficial16", "validators=", addr.Hex(), "blocknumber=", blocknumber.Uint64())
	}
	rewardAmount := GetRewardAmount(blocknumber.Uint64(), types.DREBlockReward)
	for _, owner := range validators {
		ownerObject := s.GetOrNewAccountStateObject(owner)
		if ownerObject != nil {
			log.Info("ownerobj", "addr", ownerObject.address.Hex(), "blocknumber=", blocknumber.Uint64())
			ownerObject.AddBalance(rewardAmount)
		}
	}

	// reward SNFT to exchangers
	log.Info("CreateNFTByOfficial16", "exchangers len=", len(exchangers), "blocknumber=", blocknumber.Uint64())
	for _, addr := range exchangers {
		log.Info("CreateNFTByOfficial16", "exchangers=", addr.Hex(), "blocknumber=", blocknumber.Uint64())
	}

	mintStateObject := s.GetOrNewStakerStateObject(types.MintDeepStorageAddress)

	for _, awardee := range exchangers {
		nftAddr := common.Address{}
		if mintStateObject.OfficialMint() == nil {
			log.Info("CreateNFTByOfficial16()", "blocknumber=", blocknumber.Uint64())
		}
		nftAddr = common.BytesToAddress(mintStateObject.OfficialMint().Bytes())
		log.Info("CreateNFTByOfficial16()", "--nftAddr=", nftAddr.String(), "blocknumber=", blocknumber.Uint64())
		stateObject := s.GetOrNewNFTStateObject(nftAddr)
		if stateObject != nil {
			stateObject.SetNFTInfo(
				awardee,
				awardee)

			mintStateObject.AddOfficialMint(big.NewInt(1))

		}
	}
}

func (s *StateDB) DistributeRewardsToStakers(validators []common.Address, blocknumber *big.Int) {
	rewardAmount := GetRewardAmount(blocknumber.Uint64(), types.DREBlockReward)
	stakersPercentage := 100 - types.PercentageValidatorReward
	sumStakerReward := new(big.Int).Div(new(big.Int).Mul(rewardAmount, big.NewInt(int64(stakersPercentage))), big.NewInt(100))
	for _, owner := range validators {
		ownerObject := s.GetOrNewAccountStateObject(owner)
		if ownerObject != nil {
			stakerList := ownerObject.GetValidatorExtension()
			ownerStakerBalance := stakerList.GetBalance(owner)
			sumStakerBalance := new(big.Int).Sub(stakerList.GetAllBalance(), ownerStakerBalance)
			actualSumStakerReward := big.NewInt(0)
			for _, staker := range stakerList.ValidatorExtensions {
				if staker.Addr != owner {
					stakerReward := new(big.Int).Div(new(big.Int).Mul(sumStakerReward, staker.Balance), sumStakerBalance)
					stakerObject := s.GetOrNewAccountStateObject(staker.Addr)
					stakerObject.AddBalance(stakerReward)
					actualSumStakerReward.Add(actualSumStakerReward, stakerReward)
				}
			}
			ownerObject.SubBalance(actualSumStakerReward)
		}
	}
}

func (s *StateDB) MintNFTLog(nftAddress common.Address, blockNumber *big.Int) *types.Log {
	//event MintNFT(address indexed nftaddress)
	//hash1 is MintNFT(address indexed nftaddress)
	//0x385e9e2ed650704f0fdc4ea7496f88a83ad457497f62b54efcb903a67c58a68f
	hash1 := common.HexToHash("0x385e9e2ed650704f0fdc4ea7496f88a83ad457497f62b54efcb903a67c58a68f")
	nftString := nftAddress.Hex()
	nftString = string([]byte(nftString)[2:])
	hash2 := common.HexToHash("000000000000000000000000" + nftString)
	log := &types.Log{
		Address: common.Address{},
		Topics: []common.Hash{
			hash1,
			hash2,
		},
		BlockNumber: blockNumber.Uint64(),
	}

	return log
}

func (s *StateDB) GetExchangAmount(nftaddress common.Address, initamount *big.Int) *big.Int {
	nftInt := new(big.Int).SetBytes(nftaddress.Bytes())
	baseInt, _ := big.NewInt(0).SetString("8000000000000000000000000000000000000000", 16)
	nftInt.Sub(nftInt, baseInt)
	//nftInt.Add(nftInt, big.NewInt(1))
	nftInt.Div(nftInt, big.NewInt(4096))
	times := nftInt.Uint64() / types.ExchangePeriod
	rewardratio := gomath.Pow(types.DeflationRate, float64(times))
	result := big.NewInt(0)
	new(big.Float).Mul(big.NewFloat(rewardratio), new(big.Float).SetInt(initamount)).Int(result)

	return result
}

func (s *StateDB) calculateExchangeAmount(level uint8, mergenumber uint32) *big.Int {
	//nftNumber := math.BigPow(16, int64(level))
	nftNumber := big.NewInt(int64(mergenumber))
	switch {
	case level == 0:
		radix, _ := big.NewInt(0).SetString(types.SNFTL0, 10)
		return big.NewInt(0).Mul(nftNumber, radix)
	case level == 1:
		radix, _ := big.NewInt(0).SetString(types.SNFTL1, 10)
		return big.NewInt(0).Mul(nftNumber, radix)
	case level == 2:
		radix, _ := big.NewInt(0).SetString(types.SNFTL2, 10)
		return big.NewInt(0).Mul(nftNumber, radix)
	default:
		radix, _ := big.NewInt(0).SetString(types.SNFTL3, 10)
		return big.NewInt(0).Mul(nftNumber, radix)
	}
}

func (s *StateDB) CalculateExchangeAmount(level uint8, mergenumber uint32) *big.Int {
	return s.calculateExchangeAmount(level, mergenumber)
}

// -  pledge token: a user who want to be a miner need to pledge token, must more than 100000 erb
// ````
// {
// balance:????
// data:{
// }
// }
// ````
//
//from:owner
//to:0xffff...ffff
//version:0
//type:6
func (s *StateDB) PledgeToken(address common.Address,
	amount *big.Int,
	proxy common.Address,
	blocknumber *big.Int) error {

	if amount == nil {
		amount = big.NewInt(0)
	}

	stateObject := s.GetOrNewAccountStateObject(address)

	//Resolving duplicates is delegated
	empty := common.Address{}
	validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
	validators := validatorStateObject.GetValidators()
	for _, v := range validators.Validators {
		if v.Proxy != empty && v.Addr != address && v.Proxy == proxy {
			log.Info("PledgeToken|break", "address", address, "proxy", proxy)
			return errors.New("cannot delegate repeatedly")
		}
	}

	if stateObject != nil {
		validatorStateObject.AddValidator(address, amount, proxy)

		stateObject.SubBalance(amount)
		stateObject.AddPledgedBalance(amount)
		stateObject.SetPledgedBlockNumber(blocknumber)
	}
	return nil
}

//func (s *StateDB) StakerToken(from common.Address, address common.Address, amount *big.Int) error {
//	stateObject := s.GetOrNewAccountStateObject(address)
//	fromObject := s.GetOrNewAccountStateObject(from)
//	if amount == nil {
//		amount = big.NewInt(0)
//	}
//
//	//Resolving duplicates is delegated
//	empty := common.Address{}
//	validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
//	validators := validatorStateObject.GetValidators()
//	for _, v := range validators.Validators {
//		if v.Proxy != empty && v.Addr != address && v.Proxy == proxy {
//			log.Info("PledgeToken|break", "address", address, "proxy", proxy)
//			return errors.New("cannot delegate repeatedly")
//		}
//	}
//
//	if stateObject != nil {
//		validatorStateObject.AddValidator(address, amount, proxy)
//		stateObject.SubBalance(amount)
//		stateObject.AddPledgedBalance(amount)
//		stateObject.SetPledgedBlockNumber(blocknumber)
//	}
//	return nil
//}

func (s *StateDB) StakerPledge(from common.Address, address common.Address,
	amount *big.Int, blocknumber *big.Int, wh *types.Wormholes) error {

	toObject := s.GetOrNewAccountStateObject(address)
	fromObject := s.GetOrNewAccountStateObject(from)
	//Resolving duplicates is delegated
	//validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)

	if fromObject != nil && toObject != nil {

		newProxy := common.Address{}
		if wh.ProxyAddress != "" {
			newProxy = common.HexToAddress(wh.ProxyAddress)
		}

		fromObject.SubBalance(amount)
		fromObject.StakerPledge(address, amount, blocknumber)
		toObject.AddPledgedBalance(amount)

		// If the miner pledges himself, if no proxy address is set,
		//the previously set proxy address will also be cleared
		if from == address {
			toObject.SetValidatorProxy(newProxy)
		}
		toObject.AddValidatorExtension(from, amount, blocknumber)
		fromObject.SetPledgedBlockNumber(blocknumber)

	} else {
		return errors.New("from Object or to Object null")
	}
	return nil

}

func (s *StateDB) MinerConsign(address common.Address, proxy common.Address) error {
	stateObject := s.GetOrNewAccountStateObject(address)
	empty := common.Address{}

	//Only pledged account can  to an another account
	existAddress := false
	validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
	validators := validatorStateObject.GetValidators()
	for _, v := range validators.Validators {
		if address == v.Addr {
			existAddress = true
		}
	}
	if !existAddress {
		log.Info("MinerConsign", "err", "no repeated pledge")
		return errors.New("no repeated pledge")
	}

	//Resolving duplicates is delegated
	for _, v := range validators.Validators {
		if v.Proxy != empty && v.Proxy == proxy {
			log.Info("PledgeToken|break", "address", address, "proxy", proxy)
			return errors.New("cannot delegate repeatedly")
		}
	}
	if stateObject != nil {
		validatorStateObject.AddValidator(address, big.NewInt(0), proxy)
	}
	return nil
}

func (s *StateDB) MinerBecome(address common.Address, proxy common.Address) error {
	stateObject := s.GetOrNewAccountStateObject(address)
	//empty := common.Address{}

	validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
	//validators := validatorStateObject.GetValidators()
	//for _, v := range validators.Validators {
	//	if address == v.Addr {
	//		log.Info("MinerBecome", "err", "already pledge")
	//		return errors.New("already pledge")
	//	}
	//}
	//
	////Resolving duplicates is delegated
	//for _, v := range validators.Validators {
	//	if v.Proxy != empty && v.Proxy == proxy {
	//		log.Info("PledgeToken|break", "address", address, "proxy", proxy)
	//		return errors.New("cannot delegate repeatedly")
	//	}
	//}
	if stateObject != nil {
		validatorStateObject.AddValidator(address, stateObject.PledgedBalance(), proxy)
	}
	return nil
}

func (s *StateDB) ResetMinerBecome(address common.Address) error {
	emptyAddress := common.Address{}
	stateObject := s.GetOrNewAccountStateObject(address)

	validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)

	if stateObject != nil {

		validatorStateObject.ResetemoveValidator(address)

		if stateObject.PledgedBalance().Cmp(types.ValidatorBase()) < 0 {
			return nil
		}

		// If the same proxy address has been added to the validator list,
		// the other validators with the same proxy address cannot be added to the validator list
		proxy := stateObject.GetValidatorProxy()
		if proxy != emptyAddress &&
			validatorStateObject.GetValidators().Exist(proxy) {
			return errors.New("cannot have the same proxy")
		}

		coefficient := s.GetValidatorCoefficient(address)
		if coefficient == 0 {
			s.AddValidatorCoefficient(address, VALIDATOR_COEFFICIENT)
		}

		validatorStateObject.SetValidatorAmount(address, stateObject.PledgedBalance(), proxy)
	}
	return nil
}

// - cancel pledged token
// ````
// {
// from: holder
// balance:???? amount of recall ERB
// data:{
// }
// }
// ````
//
//to:0xffff...ffff
//version:0
//type:7
func (s *StateDB) CancelPledgedToken(address common.Address, amount *big.Int) {
	stateObject := s.GetOrNewAccountStateObject(address)
	if stateObject != nil {
		validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
		validatorStateObject.RemoveValidator(address, amount)

		stateObject.SubPledgedBalance(amount)
		stateObject.AddBalance(amount)
	}
}

//func (s *StateDB) NewCancelStakerPledge(from, address common.Address, amount *big.Int, blocknumber *big.Int) error {
//
//	toObject := s.GetOrNewAccountStateObject(address)
//	fromObject := s.GetOrNewAccountStateObject(from)
//
//	if fromObject != nil && toObject != nil {
//		validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
//		stakerStateObject := s.GetOrNewStakerStateObject(types.StakerStorageAddress)
//		coebaseErb, _ := new(big.Int).SetString("100000000000000000", 10)
//		punishErb := big.NewInt(VALIDATOR_COEFFICIENT - int64(toObject.Coefficient()))
//		punishErb.Mul(punishErb, coebaseErb)
//
//		if from == address && fromObject.Coefficient() > 0 {
//			if amount.Cmp(punishErb) < 0 {
//				return errors.New("cancel pledge for insufficient punish amount")
//			}
//			fromObject.AddBalance(new(big.Int).Sub(amount, punishErb))
//			Zeroaddress := s.GetOrNewAccountStateObject(common.HexToAddress("0x0000000000000000000000000000000000000000"))
//			Zeroaddress.AddBalance(punishErb)
//			fromObject.SetCoefficient(VALIDATOR_COEFFICIENT)
//
//		} else {
//			fromObject.AddBalance(amount)
//		}
//		validatorStateObject.RemoveValidator(address, amount)
//		stakerStateObject.RemoveStaker(from, amount)
//
//		fromObject.RemoveStakerPledge(address, amount)
//		toObject.RemoveValidatorExtension(from, amount)
//		toObject.SubPledgedBalance(amount)
//		if fromObject.StakerPledgeLength() == 0 {
//			fromObject.SetExchangerInfo(false, blocknumber, 0, "", "")
//		}
//	}
//
//	return nil
//}

func (s *StateDB) NewCancelStakerPledge(from, address common.Address, amount *big.Int, blocknumber *big.Int) error {

	toObject := s.GetOrNewAccountStateObject(address)
	fromObject := s.GetOrNewAccountStateObject(from)

	if fromObject != nil && toObject != nil {
		validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
		if from == address {
			// A maximum of 6.9 ERB is deducted
			punishErb := big.NewInt(0)
			// if toObject.Coefficient() != 0
			//With a credit score of 0, miners have never been validators
			//When the miner who pledged 350 has not yet become a validator,
			//it is then revoked without deducting the erb of the credit value
			if toObject.Coefficient() != 0 {
				punishErb = big.NewInt(VALIDATOR_COEFFICIENT - int64(toObject.Coefficient()))
			}

			if punishErb.Cmp(big.NewInt(0)) > 0 {
				coebaseErb, _ := new(big.Int).SetString("100000000000000000", 10)
				punishErb.Mul(punishErb, coebaseErb)

				if amount.Cmp(punishErb) < 0 {
					return errors.New("cancel pledge for insufficient punish amount")
				}
				fromObject.AddBalance(new(big.Int).Sub(amount, punishErb))
				Zeroaddress := s.GetOrNewAccountStateObject(common.HexToAddress("0x0000000000000000000000000000000000000000"))
				Zeroaddress.AddBalance(punishErb)
				fromObject.SetCoefficient(VALIDATOR_COEFFICIENT)

			} else {
				fromObject.AddBalance(amount)
			}

			validators := fromObject.GetStakerExtension()
			pledgedAmount := validators.GetBalance(from)
			if pledgedAmount.Cmp(amount) == 0 {
				// withdraw all pledged amount
				fromObject.RemoveStakerPledge(address, amount)
				toObject.RemoveValidatorExtension(from, amount)
				toObject.SubPledgedBalance(amount)

				validatorStateObject.RemoveValidator(address, amount)

				// Revocation of pledge at current address of other staker
				s.RevocateAllStakers(address, blocknumber)

			} else {
				fromObject.RemoveStakerPledge(address, amount)
				toObject.RemoveValidatorExtension(from, amount)
				toObject.SubPledgedBalance(amount)

				validatorStateObject.RemoveValidator(address, amount)

			}

		} else {
			fromObject.AddBalance(amount)

			fromObject.RemoveStakerPledge(address, amount)
			toObject.RemoveValidatorExtension(from, amount)
			toObject.SubPledgedBalance(amount)

			validatorStateObject.RemoveValidator(address, amount)

		}

	}

	return nil
}

func (s *StateDB) RevocateAllStakers(addr common.Address, blocknumber *big.Int) {
	addrObject := s.GetOrNewAccountStateObject(addr)
	stakers := addrObject.GetValidatorExtension()

	validatorStateObject := s.GetOrNewStakerStateObject(types.ValidatorStorageAddress)

	for _, staker := range stakers.ValidatorExtensions {
		stakerObject := s.GetOrNewAccountStateObject(staker.Addr)
		stakerObject.RemoveStakerPledge(addr, staker.Balance)
		stakerObject.AddBalance(staker.Balance)
		addrObject.SubPledgedBalance(staker.Balance)

		validatorStateObject.RemoveValidator(addr, staker.Balance)

	}
	// clean up validator extension
	addrObject.SetValidatorExtension(&types.ValidatorsExtensionList{})
}

func (s *StateDB) GetStakerStorageAddress() *types.StakerList {
	stakerStateObject := s.GetOrNewStakerStateObject(types.StakerStorageAddress)
	if stakerStateObject != nil {
		stakers := stakerStateObject.GetStakers()
		return stakers
	}
	return nil
}

func (s *StateDB) GetNFTInfo(nftAddr common.Address) (
	common.Address,
	common.Address) {
	stateObject := s.GetOrNewNFTStateObject(nftAddr)
	if stateObject != nil {
		return stateObject.GetNFTInfo()
	}
	return common.Address{},
		common.Address{}
}

func (s *StateDB) GetPledgedTime(from, addr common.Address) *big.Int {
	stateObject := s.GetOrNewAccountStateObject(from)
	if stateObject != nil {
		return new(big.Int).Set(stateObject.StakerPledgedBlockNumber(addr))
	}
	return common.Big0
}

func (s *StateDB) GetStakerPledged(from, addr common.Address) *types.StakerExtension {
	stateObject := s.GetOrNewAccountStateObject(from)
	if stateObject != nil {
		for _, value := range stateObject.data.Worm.StakerExtension.StakerExtensions {
			if value.Addr == addr {
				if value.Balance == nil {
					value.Balance = big.NewInt(0)
				}
				if value.BlockNumber == nil {
					value.BlockNumber = big.NewInt(0)
				}
				return value
			}
		}
	}
	return &types.StakerExtension{BlockNumber: common.Big0, Balance: common.Big0}
}

func (s *StateDB) GetNFTCreator(addr common.Address) common.Address {
	stateObject := s.GetOrNewNFTStateObject(addr)
	if stateObject != nil {
		return stateObject.GetCreator()
	}
	return common.Address{}
}

func (s *StateDB) IsExistNFT(addr common.Address) bool {
	stateObject := s.GetOrNewNFTStateObject(addr)
	if stateObject != nil {
		return stateObject.NFTOwner() != common.Address{}
	}
	return false
}

// GetPledgedBalance retrieves the pledged balance from the given address or 0 if object not found
func (s *StateDB) GetPledgedBalance(addr common.Address) *big.Int {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		pledgedBalance := stateObject.PledgedBalance()
		if pledgedBalance != nil {
			return pledgedBalance
		} else {
			return common.Big0
		}
	}
	return common.Big0
}

func (s *StateDB) GetStakerPledgedBalance(from, addr common.Address) *big.Int {
	stateObject := s.GetOrNewAccountStateObject(from)
	if stateObject != nil {
		for _, value := range stateObject.data.Worm.StakerExtension.StakerExtensions {
			if value.Addr == addr {
				if value.Balance == nil {
					return common.Big0
				}
				return value.Balance
			}
		}
	}
	return common.Big0
}

func (s *StateDB) GetAccountInfo(addr common.Address) Account {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		return stateObject.GetAccountInfo()
	}
	return Account{}
}

// GetCoefficient retrieves the coefficient from the given address or 0 if object not found
func (s *StateDB) GetCoefficient(addr common.Address) uint8 {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		return stateObject.Coefficient()
	}
	return 0
}

// AddValidatorCoefficient adds amount to the ValidatorCoefficient associated with addr.
func (s *StateDB) AddValidatorCoefficient(addr common.Address, coe uint8) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.AddCoefficient(coe)
	}
}

// SubValidatorCoefficient subtracts amount from the ValidatorCoefficient associated with addr.
func (s *StateDB) SubValidatorCoefficient(addr common.Address, coe uint8) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.SubCoefficient(coe)
	}
}

func (s *StateDB) RemoveValidatorCoefficient(addr common.Address) {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		stateObject.RemoveCoefficient()
	}
}

// GetValidatorCoefficient retrieves the ValidatorCoefficient from the given address or 0 if object not found
func (s *StateDB) GetValidatorCoefficient(addr common.Address) uint8 {
	stateObject := s.GetOrNewAccountStateObject(addr)
	if stateObject != nil {
		coe := stateObject.Coefficient()
		return coe
	}
	return 0
}

func (s *StateDB) GetStakers(addr common.Address) *types.StakerList {
	stakerStateObject := s.GetOrNewStakerStateObject(addr)
	if stakerStateObject != nil {
		stakers := stakerStateObject.GetStakers()
		return stakers
	}

	return nil
}

func (s *StateDB) GetValidators(addr common.Address) *types.ValidatorList {
	validatorStateObject := s.GetOrNewStakerStateObject(addr)
	if validatorStateObject != nil {
		validators := validatorStateObject.GetValidators()
		return validators
	}

	return nil
}

func (s *StateDB) GetOfficialMint() *big.Int {
	mintStateObject := s.GetOrNewStakerStateObject(types.MintDeepStorageAddress)
	if mintStateObject != nil {
		officialMint := mintStateObject.OfficialMint()
		return new(big.Int).Set(officialMint)
	}

	return nil
}

func (s *StateDB) GetUserMint() *big.Int {
	mintStateObject := s.GetOrNewStakerStateObject(types.MintDeepStorageAddress)
	if mintStateObject != nil {
		userMint := mintStateObject.UserMint()
		return new(big.Int).Set(userMint)
	}

	return nil
}

func (s *StateDB) ChangeValidatorProxy(addr common.Address, newValidatorProxy common.Address) {
	accountStateObject := s.GetOrNewAccountStateObject(addr)
	if accountStateObject != nil {
		accountStateObject.SetValidatorProxy(newValidatorProxy)
	}
}

func (s *StateDB) GetValidatorProxy(addr common.Address) common.Address {
	accountStateObject := s.GetOrNewAccountStateObject(addr)
	if accountStateObject != nil {
		return accountStateObject.GetValidatorProxy()
	}

	return common.Address{}
}

func (s *StateDB) PunishEvilValidators(evilValidators []common.Address, blocknumber *big.Int) error {
	if len(evilValidators) == 0 {
		return nil
	}

	for _, evil := range evilValidators {
		s.SubValidatorCoefficient(evil, types.DEFAULT_VALIDATOR_COEFFICIENT)
	}

	return nil
}
