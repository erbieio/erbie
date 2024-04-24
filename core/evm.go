// Copyright 2016 The go-ethereum Authors
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

package core

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

var ErrRecoverAddress = errors.New("recover ExchangerAuth error")
var ErrNotMatchAddress = errors.New("recovered address not match exchanger owner")

const InjectRewardRate = 1000 // InjectRewardRate is 10%
var InjectRewardAddress = common.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")
var DiscardAddress = common.HexToAddress("0x0000000000000000000000000000000000000000")

const VALIDATOR_COEFFICIENT = 70

// ChainContext supports retrieving headers and consensus parameters from the
// current blockchain to be used during transaction processing.
type ChainContext interface {
	// Engine retrieves the chain's consensus engine.
	Engine() consensus.Engine

	// GetHeader returns the hash corresponding to their hash.
	GetHeader(common.Hash, uint64) *types.Header
}

// NewEVMBlockContext creates a new context for use in the EVM.
func NewEVMBlockContext(header *types.Header, chain ChainContext, author *common.Address) vm.BlockContext {
	var (
		beneficiary common.Address
		baseFee     *big.Int
	)

	// If we don't have an explicit author (i.e. not mining), extract from the header
	if author == nil {
		beneficiary, _ = chain.Engine().Author(header) // Ignore error, we're past header validation
	} else {
		beneficiary = *author
	}
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	return vm.BlockContext{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     GetHashFn(header, chain),
		Coinbase:    beneficiary,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        new(big.Int).SetUint64(header.Time),
		Difficulty:  new(big.Int).Set(header.Difficulty),
		BaseFee:     baseFee,
		GasLimit:    header.GasLimit,

		ParentHeader: chain.GetHeader(header.ParentHash, header.Number.Uint64()),

		// *** modify to support nft transaction 20211215 begin ***
		VerifyCSBTOwner: VerifyCSBTOwner,
		TransferCSBT:    TransferCSBT,
		// *** modify to support nft transaction 20211215 end ***
		PledgeToken:                         PledgeToken,
		StakerPledge:                        StakerPledge,
		GetPledgedTime:                      GetPledgedTime,
		GetStakerPledged:                    GetStakerPledged,
		MinerConsign:                        MinerConsign,
		MinerBecome:                         MinerBecome,
		IsExistOtherPledged:                 IsExistOtherPledged,
		ResetMinerBecome:                    ResetMinerBecome,
		CancelPledgedToken:                  CancelPledgedToken,
		NewCancelStakerPledge:               NewCancelStakerPledge,
		GetNFTCreator:                       GetNFTCreator,
		IsExistNFT:                          IsExistNFT,
		VerifyPledgedBalance:                VerifyPledgedBalance,
		VerifyStakerPledgedBalance:          VerifyStakerPledgedBalance,
		VerifyCancelValidatorPledgedBalance: VerifyCancelValidatorPledgedBalance,
		GetNftAddressAndLevel:               GetNftAddressAndLevel,
		RecoverValidatorCoefficient:         RecoverValidatorCoefficient,
		IsExistStakerStorageAddress:         IsExistStakerStorageAddress,
	}
}

// NewEVMTxContext creates a new transaction context for a single transaction.
func NewEVMTxContext(msg Message) vm.TxContext {
	return vm.TxContext{
		Origin:   msg.From(),
		GasPrice: new(big.Int).Set(msg.GasPrice()),
	}
}

// GetHashFn returns a GetHashFunc which retrieves header hashes by number
func GetHashFn(ref *types.Header, chain ChainContext) func(n uint64) common.Hash {
	// Cache will initially contain [refHash.parent],
	// Then fill up with [refHash.p, refHash.pp, refHash.ppp, ...]
	var cache []common.Hash

	return func(n uint64) common.Hash {
		// If there's no hash cache yet, make one
		if len(cache) == 0 {
			cache = append(cache, ref.ParentHash)
		}
		if idx := ref.Number.Uint64() - n - 1; idx < uint64(len(cache)) {
			return cache[idx]
		}
		// No luck in the cache, but we can start iterating from the last element we already know
		lastKnownHash := cache[len(cache)-1]
		lastKnownNumber := ref.Number.Uint64() - uint64(len(cache))

		for {
			header := chain.GetHeader(lastKnownHash, lastKnownNumber)
			if header == nil {
				break
			}
			cache = append(cache, header.ParentHash)
			lastKnownHash = header.ParentHash
			lastKnownNumber = header.Number.Uint64() - 1
			if n == lastKnownNumber {
				return lastKnownHash
			}
		}
		return common.Hash{}
	}
}

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func CanTransfer(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}

// *** modify to support nft transaction 20211215 begin ***

// CanTransferCSBT checks whether the owner own the csbt
func VerifyCSBTOwner(db vm.StateDB, nftAddr string, owner common.Address) bool {
	address, _, err := GetNftAddressAndLevel(nftAddr)
	if err != nil {
		return false
	}
	returnOwner := db.GetNFTOwner16(address)
	//fmt.Println("nftAddr=", nftAddr)
	//fmt.Println("fact owner=", returnOwner.String())
	//fmt.Println("expected owner=", owner.String())
	//fmt.Println("is the same owner=", returnOwner == owner)
	return returnOwner == owner
	//return db.GetNFTOwner(nftAddr) == owner
}

// GetNftAddressAndLevel is to 16 version
func GetNftAddressAndLevel(nftAddress string) (common.Address, int, error) {
	if len(nftAddress) > 42 {
		return common.Address{}, 0, errors.New("nft address is too long")
	}
	level := 0
	if strings.HasPrefix(nftAddress, "0x") ||
		strings.HasPrefix(nftAddress, "0X") {
		level = 42 - len(nftAddress)
	} else {
		return common.Address{}, 0, errors.New("nft address is not to start with 0x")
	}

	for i := 0; i < level; i++ {
		nftAddress = nftAddress + "0"
	}

	address := common.HexToAddress(nftAddress)

	return address, level, nil
}

// TransferCSBT change the NFT's owner
func TransferCSBT(db vm.StateDB, nftAddr string, newOwner common.Address, blocknumber *big.Int) error {
	address, level, err := GetNftAddressAndLevel(nftAddr)
	if err != nil {
		return err
	}

	db.ChangeNFTOwner(address, newOwner, level, blocknumber)
	return nil
}

// *** modify to support nft transaction 20211215 end ***

func PledgeToken(db vm.StateDB, address common.Address, amount *big.Int, wh *types.Wormholes, blocknumber *big.Int) error {
	empty := common.Address{}
	log.Info("PledgeToken", "proxy", wh.ProxyAddress, "sign", wh.ProxySign)
	if wh.ProxyAddress != "" && wh.ProxyAddress != empty.Hex() {
		msg := fmt.Sprintf("%v%v", wh.ProxyAddress, address.Hex())
		addr, err := RecoverAddress(msg, wh.ProxySign)
		log.Info("PledgeToken", "proxy", wh.ProxyAddress, "addr", addr, "sign", wh.ProxySign)
		if err != nil || wh.ProxyAddress != addr.Hex() {
			log.Error("PledgeToken()", "Get public key error", err)
			return errors.New("recover proxy address error!")
		}
	}

	return db.PledgeToken(address, amount, common.HexToAddress(wh.ProxyAddress), blocknumber)
}

func StakerPledge(db vm.StateDB, from, address common.Address, amount *big.Int, blocknumber *big.Int, wh *types.Wormholes) error {
	return db.StakerPledge(from, address, amount, blocknumber, wh)
}

func GetPledgedTime(db vm.StateDB, from, addr common.Address) *big.Int {
	return db.GetPledgedTime(from, addr)
}
func GetStakerPledged(db vm.StateDB, from, addr common.Address) *types.StakerExtension {
	return db.GetStakerPledged(from, addr)
}

func MinerConsign(db vm.StateDB, address common.Address, wh *types.Wormholes) error {
	msg := fmt.Sprintf("%v%v", wh.ProxyAddress, address.Hex())
	addr, err := RecoverAddress(msg, wh.ProxySign)
	log.Info("MinerConsign", "proxy", wh.ProxyAddress, "addr", addr, "sign", wh.ProxySign)
	if err != nil || wh.ProxyAddress != addr.Hex() {
		log.Error("MinerConsign()", "Get public key error", err)
		return err
	}
	return db.MinerConsign(address, common.HexToAddress(wh.ProxyAddress))
}

func MinerBecome(db vm.StateDB, address common.Address, proxy common.Address) error {
	//msg := fmt.Sprintf("%v%v", wh.ProxyAddress, address.Hex())
	//addr, err := RecoverAddress(msg, wh.ProxySign)
	//log.Info("MinerBecome", "proxy", wh.ProxyAddress, "addr", addr, "sign", wh.ProxySign)
	//if err != nil {
	//	log.Error("MinerBecome()", "Get public key error", err)
	//	return err
	//}
	//if wh.ProxyAddress != addr.Hex() {
	//	log.Error("MinerBecome()", "Get public key error", err)
	//	return errors.New("MinerBecome recover address proxy Address != address")
	//}
	return db.MinerBecome(address, proxy)
}

func IsExistOtherPledged(db vm.StateDB, address common.Address) bool {

	pledgeBalance := db.GetPledgedBalance(address)
	stakerBalance := db.GetStakerPledgedBalance(address, address)
	if pledgeBalance.Cmp(common.Big0) == 0 {
		return false
	}
	if pledgeBalance.Cmp(stakerBalance) != 0 {
		return true
	}
	return false
}

func ResetMinerBecome(db vm.StateDB, address common.Address) error {
	return db.ResetMinerBecome(address)
}

func CancelPledgedToken(db vm.StateDB, address common.Address, amount *big.Int) {
	db.CancelPledgedToken(address, amount)
}

func NewCancelStakerPledge(db vm.StateDB, from common.Address, address common.Address, amount *big.Int, blocknumber *big.Int) error {
	return db.NewCancelStakerPledge(from, address, amount, blocknumber)
}

func GetNFTCreator(db vm.StateDB, addr common.Address) common.Address {
	return db.GetNFTCreator(addr)
}

func IsExistNFT(db vm.StateDB, addr common.Address) bool {
	return db.IsExistNFT(addr)
}

// VerifyPledgedBalance checks whether there are enough pledged funds in the address' account to make CancelPledgeToken().
// This does not take the necessary gas in to account to make the transfer valid.
func VerifyPledgedBalance(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	return db.GetPledgedBalance(addr).Cmp(amount) >= 0
}

func VerifyStakerPledgedBalance(db vm.StateDB, from common.Address, addr common.Address, amount *big.Int) bool {
	return db.GetStakerPledgedBalance(from, addr).Cmp(amount) >= 0
}

func IsExistStakerStorageAddress(db vm.StateDB, address common.Address) bool {
	pdcs := db.GetStakerStorageAddress()

	return pdcs.IsExist(address)
}

func VerifyCancelValidatorPledgedBalance(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	return new(big.Int).Sub(db.GetStakerPledged(addr, addr).Balance, amount).
		Cmp(new(big.Int).Sub(db.GetPledgedBalance(addr), db.GetStakerPledged(addr, addr).Balance)) >= 0

}

// hashMsg return the hash of plain msg
func hashMsg(data []byte) ([]byte, string) {
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(data), string(data))
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(msg))
	return hasher.Sum(nil), msg
}

// recoverAddress recover the address from sig
func RecoverAddress(msg string, sigStr string) (common.Address, error) {
	if !strings.HasPrefix(sigStr, "0x") &&
		!strings.HasPrefix(sigStr, "0X") {
		return common.Address{}, fmt.Errorf("signature must be started with 0x or 0X")
	}
	sigData, err := hexutil.Decode(sigStr)
	if err != nil {
		return common.Address{}, err
	}
	if len(sigData) != 65 {
		return common.Address{}, fmt.Errorf("signature must be 65 bytes long")
	}
	if sigData[64] != 27 && sigData[64] != 28 {
		return common.Address{}, fmt.Errorf("invalid Ethereum signature (V is not 27 or 28)")
	}
	sigData[64] -= 27
	hash, _ := hashMsg([]byte(msg))
	//fmt.Println("sigdebug hash=", hexutil.Encode(hash))
	rpk, err := crypto.SigToPub(hash, sigData)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*rpk), nil
}

func RecoverValidatorCoefficient(db vm.StateDB, address common.Address) error {
	balance := db.GetPledgedBalance(address)
	if balance.Cmp(big.NewInt(0)) <= 0 {
		return errors.New("not a validator")
	}
	coe := db.GetValidatorCoefficient(address)
	if coe == 0 {
		return errors.New("Get validator coefficient error")
	}
	needRecoverCoe := VALIDATOR_COEFFICIENT - coe
	if needRecoverCoe > 0 {
		recoverAmount := new(big.Int).Mul(big.NewInt(int64(needRecoverCoe)), big.NewInt(10000000000000000))
		if db.GetBalance(address).Cmp(recoverAmount) < 0 {
			return errors.New("insufficient balance for transfer")
		}
		db.SubBalance(address, recoverAmount)
		db.AddBalance(common.HexToAddress("0x0000000000000000000000000000000000000000"), recoverAmount)
		db.AddValidatorCoefficient(address, needRecoverCoe)
	}

	return nil
}

func checkBlockNumber(wBlockNumber string, currentBlockNumber *big.Int) error {
	if !strings.HasPrefix(wBlockNumber, "0x") &&
		!strings.HasPrefix(wBlockNumber, "0X") {
		return errors.New("blocknumber is not string of 0x!")
	}
	blockNumber, ok := new(big.Int).SetString(wBlockNumber[2:], 16)
	if !ok {
		return errors.New("blocknumber is not string of 0x!")
	}
	if currentBlockNumber.Cmp(blockNumber) > 0 {
		return errors.New("data is expired!")
	}

	return nil
}
