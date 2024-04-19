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
		VerifyNFTOwner: VerifyNFTOwner,
		TransferNFT:    TransferNFT,
		// *** modify to support nft transaction 20211215 end ***
		PledgeToken:           PledgeToken,
		StakerPledge:          StakerPledge,
		GetPledgedTime:        GetPledgedTime,
		GetStakerPledged:      GetStakerPledged,
		MinerConsign:          MinerConsign,
		MinerBecome:           MinerBecome,
		IsExistOtherPledged:   IsExistOtherPledged,
		ResetMinerBecome:      ResetMinerBecome,
		CancelPledgedToken:    CancelPledgedToken,
		NewCancelStakerPledge: NewCancelStakerPledge,
		OpenExchanger:         OpenExchanger,
		CloseExchanger:        CloseExchanger,
		GetExchangerFlag:      GetExchangerFlag,
		GetOpenExchangerTime:  GetOpenExchangerTime,
		GetFeeRate:            GetFeeRate,
		GetExchangerName:      GetExchangerName,
		GetExchangerURL:       GetExchangerURL,
		GetApproveAddress:     GetApproveAddress,
		//GetNFTBalance:                      GetNFTBalance,
		GetNFTName:                          GetNFTName,
		GetNFTSymbol:                        GetNFTSymbol,
		GetNFTApproveAddress:                GetNFTApproveAddress,
		GetNFTMergeLevel:                    GetNFTMergeLevel,
		GetNFTCreator:                       GetNFTCreator,
		GetNFTRoyalty:                       GetNFTRoyalty,
		GetNFTExchanger:                     GetNFTExchanger,
		GetNFTMetaURL:                       GetNFTMetaURL,
		IsExistNFT:                          IsExistNFT,
		IsApproved:                          IsApproved,
		IsApprovedOne:                       IsApprovedOne,
		IsApprovedForAll:                    IsApprovedForAll,
		VerifyPledgedBalance:                VerifyPledgedBalance,
		VerifyStakerPledgedBalance:          VerifyStakerPledgedBalance,
		VerifyCancelValidatorPledgedBalance: VerifyCancelValidatorPledgedBalance,
		InjectOfficialNFT:                   InjectOfficialNFT,
		AddExchangerToken:                   AddExchangerToken,
		ModifyOpenExchangerTime:             ModifyOpenExchangerTime,
		SubExchangerToken:                   SubExchangerToken,
		SubExchangerBalance:                 SubExchangerBalance,
		VerifyExchangerBalance:              VerifyExchangerBalance,
		GetNftAddressAndLevel:               GetNftAddressAndLevel,
		VoteOfficialNFT:                     VoteOfficialNFT,
		ElectNominatedOfficialNFT:           ElectNominatedOfficialNFT,
		NextIndex:                           NextIndex,
		VoteOfficialNFTByApprovedExchanger:  VoteOfficialNFTByApprovedExchanger,
		//ChangeRewardFlag:                   ChangeRewardFlag,
		//PledgeNFT:                   PledgeNFT,
		//CancelPledgedNFT:            CancelPledgedNFT,
		GetMergeNumber: GetMergeNumber,
		//GetPledgedFlag:              GetPledgedFlag,
		//GetNFTPledgedBlockNumber:    GetNFTPledgedBlockNumber,
		RecoverValidatorCoefficient: RecoverValidatorCoefficient,
		IsExistStakerStorageAddress: IsExistStakerStorageAddress,
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

// CanTransferNFT checks whether the owner own the nft
func VerifyNFTOwner(db vm.StateDB, nftAddr string, owner common.Address) bool {
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

// TransferNFT change the NFT's owner
func TransferNFT(db vm.StateDB, nftAddr string, newOwner common.Address, blocknumber *big.Int) error {
	address, level, err := GetNftAddressAndLevel(nftAddr)
	if err != nil {
		return err
	}

	level2 := db.GetNFTMergeLevel(address)
	if level != int(level2) {
		return errors.New("not exist nft")
	}

	//pledgedFlag := db.GetPledgedFlag(address)
	//if pledgedFlag {
	//	return errors.New("has been pledged")
	//}

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
	log.Info("staker fee rate =", fmt.Sprint(wh.FeeRate), "&wormholes =", fmt.Sprint(wh))
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

func OpenExchanger(db vm.StateDB,
	addr common.Address,
	amount *big.Int,
	blocknumber *big.Int,
	feerate uint16,
	exchangername string,
	exchangerurl string) {

	db.OpenExchanger(addr, amount, blocknumber, feerate, exchangername, exchangerurl)
}

func CloseExchanger(db vm.StateDB,
	addr common.Address,
	blocknumber *big.Int) {
	db.CloseExchanger(addr, blocknumber)
}

func GetExchangerFlag(db vm.StateDB, addr common.Address) bool {
	return db.GetExchangerFlag(addr)
}

func GetOpenExchangerTime(db vm.StateDB, addr common.Address) *big.Int {
	return db.GetOpenExchangerTime(addr)
}

func GetFeeRate(db vm.StateDB, addr common.Address) uint16 {
	return db.GetFeeRate(addr)
}

func GetExchangerName(db vm.StateDB, addr common.Address) string {
	return db.GetExchangerName(addr)
}

func GetExchangerURL(db vm.StateDB, addr common.Address) string {
	return db.GetExchangerURL(addr)
}

func GetApproveAddress(db vm.StateDB, addr common.Address) []common.Address {
	return db.GetApproveAddress(addr)
}

//func GetNFTBalance(db vm.StateDB, addr common.Address) uint64 {
//	return db.GetNFTBalance(addr)
//}

func GetNFTName(db vm.StateDB, addr common.Address) string {
	return db.GetNFTName(addr)
}

func GetNFTSymbol(db vm.StateDB, addr common.Address) string {
	return db.GetNFTSymbol(addr)
}

//	func GetNFTApproveAddress(db vm.StateDB, addr common.Address) []common.Address {
//		return db.GetNFTApproveAddress(addr)
//	}
func GetNFTApproveAddress(db vm.StateDB, addr common.Address) common.Address {
	return db.GetNFTApproveAddress(addr)
}

func GetNFTMergeLevel(db vm.StateDB, addr common.Address) uint8 {
	return db.GetNFTMergeLevel(addr)
}

func GetNFTCreator(db vm.StateDB, addr common.Address) common.Address {
	return db.GetNFTCreator(addr)
}

func GetNFTRoyalty(db vm.StateDB, addr common.Address) uint16 {
	return db.GetNFTRoyalty(addr)
}

func GetNFTExchanger(db vm.StateDB, addr common.Address) common.Address {
	return db.GetNFTExchanger(addr)
}

func GetNFTMetaURL(db vm.StateDB, addr common.Address) string {
	return db.GetNFTMetaURL(addr)
}

func IsExistNFT(db vm.StateDB, addr common.Address) bool {
	return db.IsExistNFT(addr)
}

func IsApproved(db vm.StateDB, nftAddr common.Address, addr common.Address) bool {
	return db.IsApproved(nftAddr, addr)
}
func IsApprovedOne(db vm.StateDB, nftAddr common.Address, addr common.Address) bool {
	return db.IsApprovedOne(nftAddr, addr)
}

func IsApprovedForAll(db vm.StateDB, ownerAddr common.Address, addr common.Address) bool {
	return db.IsApprovedForAll(ownerAddr, addr)
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

func InjectOfficialNFT(db vm.StateDB, dir string, startIndex *big.Int, number uint64, royalty uint16, creator string) {
	db.InjectOfficialNFT(dir, startIndex, number, royalty, creator)
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

func CheckSeller1(db vm.StateDB,
	blocknumber *big.Int,
	caller common.Address,
	to common.Address,
	wormholes *types.Wormholes,
	amount *big.Int) bool {

	//1. recove seller address
	msg := wormholes.Seller1.Amount +
		wormholes.Seller1.NFTAddress +
		wormholes.Seller1.Exchanger +
		wormholes.Seller1.BlockNumber
	seller, err := RecoverAddress(msg, wormholes.Seller1.Sig)
	if err != nil {
		log.Error("CheckSeller1()", "Get public key error", err)
		return false
	}

	err = checkBlockNumber(wormholes.Seller1.BlockNumber, blocknumber)
	if err != nil {
		log.Error("CheckSeller1(), seller data", "error", err)
		return false
	}

	nftAddress, level, err := GetNftAddressAndLevel(wormholes.Seller1.NFTAddress)
	if err != nil {
		log.Error("CheckSeller1(), nft address error", "wormholes.Buyer.NFTAddress", wormholes.Buyer.NFTAddress)
		return false
	}
	level2 := db.GetNFTMergeLevel(nftAddress)
	if int(level2) != level {
		log.Error("CheckSeller1()", "nft address", wormholes.Seller1.NFTAddress,
			"input nft level", level, "real nft level", level2)
		return false
	}
	//pledgedFlag := db.GetPledgedFlag(nftAddress)
	//if pledgedFlag {
	//	return false
	//}
	nftOwner := db.GetNFTOwner16(nftAddress)
	emptyAddress := common.Address{}
	if nftOwner == emptyAddress {
		log.Error("CheckSeller1(), Get nft owner error!")
		return false
	}

	if seller != nftOwner {
		return false
	}

	return true
}

// BuyNFTByApproveExchanger is tx that approved exchanger
func BuyNFTByApproveExchanger(
	db vm.StateDB,
	blocknumber *big.Int,
	caller common.Address,
	to common.Address,
	wormholes *types.Wormholes,
	amount *big.Int) error {

	//1. recover buyer's address
	msg := wormholes.Buyer.Amount +
		wormholes.Buyer.NFTAddress +
		wormholes.Buyer.Exchanger +
		wormholes.Buyer.BlockNumber +
		wormholes.Buyer.Seller
	//msgHash := crypto.Keccak256([]byte(msg))
	//sig, _ := hex.DecodeString(wormholes.Buyer.Sig)
	//pubKey, err := crypto.SigToPub(msgHash, sig)
	//if err != nil {
	//	log.Info("BuyNFTByApproveExchanger()", "Get buyer public key error", err)
	//	return err
	//}
	//buyer := crypto.PubkeyToAddress(*pubKey)
	buyer, err := RecoverAddress(msg, wormholes.Buyer.Sig)
	if err != nil {
		log.Error("BuyNFTByApproveExchanger()", "Get buyer public key error", err)
		return err
	}

	exchangerMsg := wormholes.ExchangerAuth.ExchangerOwner +
		wormholes.ExchangerAuth.To +
		wormholes.ExchangerAuth.BlockNumber
	//exchangerMsgHash := crypto.Keccak256([]byte(exchangerMsg))
	//exchangerSig, _ := hex.DecodeString(wormholes.ExchangerAuth.Sig)
	//exchangerPubKey, err := crypto.SigToPub(exchangerMsgHash, exchangerSig)
	//if err != nil {
	//	log.Info("BuyNFTByApproveExchanger()", "Get exchanger public key error", err)
	//	return err
	//}
	//originalExchanger := crypto.PubkeyToAddress(*exchangerPubKey)
	originalExchanger, err := RecoverAddress(exchangerMsg, wormholes.ExchangerAuth.Sig)
	if err != nil {
		log.Error("BuyNFTByApproveExchanger()", "Get exchanger public key error", err)
		return ErrRecoverAddress
	}
	if originalExchanger != common.HexToAddress(wormholes.ExchangerAuth.ExchangerOwner) {
		return ErrNotMatchAddress
	}

	//2. compare current block number and buyer.blocknumber and exchanger_auth.blocknumber,
	//return error if current block number is greater than buyer.blocknumber and exchanger_auth.blocknumber.
	if !strings.HasPrefix(wormholes.Buyer.BlockNumber, "0x") &&
		!strings.HasPrefix(wormholes.Buyer.BlockNumber, "0X") {
		log.Error("BuyNFTByApproveExchanger(), buyer blocknumber format  error",
			"wormholes.Buyer.BlockNumber", wormholes.Buyer.BlockNumber)
		return errors.New("buyer blocknumber is not string of 0x!")
	}
	buyerBlockNumber, ok := new(big.Int).SetString(wormholes.Buyer.BlockNumber[2:], 16)
	if !ok {
		log.Error("BuyNFTByApproveExchanger(), buyer blocknumber format  error", "ok", ok)
		return errors.New("buyer blocknumber is not string of 0x!")
	}
	if blocknumber.Cmp(buyerBlockNumber) > 0 {
		log.Error("BuyNFTByApproveExchanger(), buyer's data is expired!",
			"buyerBlockNumber", buyerBlockNumber.Text(16), "blocknumber", blocknumber.Text(16))
		return errors.New("buyer's data is expired!")
	}

	if !strings.HasPrefix(wormholes.ExchangerAuth.BlockNumber, "0x") &&
		!strings.HasPrefix(wormholes.ExchangerAuth.BlockNumber, "0X") {
		log.Error("BuyNFTByApproveExchanger(), exchanger blocknumber format error",
			"wormholes.ExchangerAuth.BlockNumber", wormholes.ExchangerAuth.BlockNumber)
		return errors.New("exchanger blocknumber is not string of 0x!")
	}
	exchangerBlockNumber, ok := new(big.Int).SetString(wormholes.ExchangerAuth.BlockNumber[2:], 16)
	if !ok {
		log.Error("BuyNFTByApproveExchanger(), auth exchanger blocknumber format error", "ok", ok)
		return errors.New("exchanger blocknumber is not string of 0x!")
	}
	if blocknumber.Cmp(exchangerBlockNumber) > 0 {
		log.Error("BuyNFTByApproveExchanger(), exchanger's data is expired!",
			"exchangerBlockNumber", exchangerBlockNumber.Text(16), "blocknumber", blocknumber.Text(16))
		return errors.New("exchanger's data is expired!")
	}

	//3. check buyer's address and to address as well as exchanger_auth.to and sender ,
	//return error if they are not same.
	if to != buyer {
		log.Error("BuyNFTByApproveExchanger(), to of the tx is not buyer!",
			"to", to.String(), "buyer", buyer.String())
		return errors.New("to of the tx is not buyer!")
	}

	approvedAddr := common.HexToAddress(wormholes.ExchangerAuth.To)
	if approvedAddr != caller {
		log.Error("BuyNFTByApproveExchanger(), from of the tx is not approved!",
			"caller", caller.String(), "wormholes.ExchangerAuth.To", wormholes.ExchangerAuth.To)
		return errors.New("from of the tx is not approved!")
	}

	//4. return error if the amount that sender send is not equal buyer's amount.
	if !strings.HasPrefix(wormholes.Buyer.Amount, "0x") &&
		!strings.HasPrefix(wormholes.Buyer.Amount, "0X") {
		log.Error("BuyNFTByApproveExchanger(), amount format error", "wormholes.Buyer.Amount", wormholes.Buyer.Amount)
		return errors.New("amount is not string of 0x!")
	}
	buyerAmount, ok := new(big.Int).SetString(wormholes.Buyer.Amount[2:], 16)
	if !ok {
		log.Error("BuyNFTByApproveExchanger(), amount format error", "ok", ok)
		return errors.New("amount is not string of 0x!")
	}
	if amount.Cmp(buyerAmount) != 0 {
		log.Error("BuyNFTByApproveExchanger(), tx amount error",
			"buyerAmount", buyerAmount.Text(16), "amount", amount.Text(16))
		return errors.New("tx amount error")
	}
	// 5.
	//nftAddress := common.HexToAddress(wormholes.Buyer.NFTAddress)
	nftAddress, level, err := GetNftAddressAndLevel(wormholes.Buyer.NFTAddress)
	if err != nil {
		log.Error("BuyNFTByApproveExchanger(), nft address error", "wormholes.Buyer.NFTAddress", wormholes.Buyer.NFTAddress)
		return err
	}
	level2 := db.GetNFTMergeLevel(nftAddress)
	if int(level2) != level {
		log.Error("BuyNFTByApproveExchanger()", "wormholes.Type", wormholes.Type, "nft address", wormholes.Buyer.NFTAddress,
			"input nft level", level, "real nft level", level2)
		return errors.New("not exist nft")
	}
	sellerNftAddress, _, err := GetNftAddressAndLevel(wormholes.Seller1.NFTAddress)
	if err != nil {
		log.Error("BuyNFTByApproveExchanger(), nft address error", "wormholes.Seller1.NFTAddress", wormholes.Seller1.NFTAddress)
		return err
	}
	if nftAddress != sellerNftAddress {
		log.Error("BuyNFTByApproveExchanger(), the nft address is not same from buyer and seller!",
			"buyerNftAddress", nftAddress.String(), "sellerNftAddress", sellerNftAddress.String())
		return errors.New("the nft address is not same from buyer and seller!")
	}
	//pledgedFlag := db.GetPledgedFlag(nftAddress)
	//if pledgedFlag {
	//	return errors.New("has been pledged")
	//}
	//buyerExchanger := common.HexToAddress(wormholes.Buyer.Exchanger)
	nftOwner := db.GetNFTOwner16(nftAddress)
	emptyAddress := common.Address{}
	if nftOwner == emptyAddress {
		log.Error("BuyNFTByApproveExchanger(), Get nft owner error!", "nftAddress", nftAddress.String())
		return errors.New("Get nft owner error!")
	}
	buyerBalance := db.GetBalance(buyer)
	//5.1 check if the buyer has sufficient balance.
	if buyerBalance.Cmp(amount) < 0 {
		log.Error("BuyNFTByApproveExchanger(), insufficient balance",
			"buyerBalance", buyerBalance.Text(16), "amount", amount.Text(16))
		return errors.New("insufficient balance")
	}

	var beneficiaryExchanger common.Address
	exclusiveExchanger := db.GetNFTExchanger(nftAddress)
	if CheckSeller1(db, blocknumber, caller, to, wormholes, amount) { //check the exchanger is or not approved exchanger,
		if exclusiveExchanger != emptyAddress {
			if originalExchanger != exclusiveExchanger {
				if db.GetExchangerFlag(exclusiveExchanger) {
					log.Error("BuyNFTByApproveExchanger(), caller not same as created exclusive Exchanger!",
						"originalExchanger", originalExchanger.String(), "exclusiveExchanger", exclusiveExchanger.String())
					return errors.New("caller not same as created exclusive Exchanger!")
				}
			}
		}

		beneficiaryExchanger = originalExchanger
	} else {
		log.Error("BuyNFTByApproveExchanger(), no right to sell nft!")
		return errors.New("no right to sell nft")
	}

	if !db.GetExchangerFlag(beneficiaryExchanger) {
		log.Error("BuyNFTByApproveExchanger(), not a exchager",
			"beneficiaryExchanger", beneficiaryExchanger.String())
		return errors.New("not a exchanger")
	}

	unitAmount := new(big.Int).Div(amount, new(big.Int).SetInt64(10000))
	feeRate := db.GetFeeRate(beneficiaryExchanger)
	exchangerAmount := new(big.Int).Mul(unitAmount, new(big.Int).SetUint64(uint64(feeRate)))
	creator := db.GetNFTCreator(nftAddress)
	royalty := db.GetNFTRoyalty(nftAddress)
	royaltyAmount := new(big.Int).Mul(unitAmount, new(big.Int).SetUint64(uint64(royalty)))
	feeAmount := new(big.Int).Add(exchangerAmount, royaltyAmount)
	nftOwnerAmount := new(big.Int).Sub(amount, feeAmount)
	db.SubBalance(buyer, amount)
	db.AddBalance(nftOwner, nftOwnerAmount)
	db.AddBalance(creator, royaltyAmount)
	//db.AddBalance(beneficiaryExchanger, exchangerAmount)
	//db.AddVoteWeight(beneficiaryExchanger, amount)
	db.ChangeNFTOwner(nftAddress, buyer, level, blocknumber)

	//mulRewardRate := new(big.Int).Mul(exchangerAmount, new(big.Int).SetInt64(InjectRewardRate))
	//injectRewardAmount := new(big.Int).Div(mulRewardRate, new(big.Int).SetInt64(10000))
	//exchangerAmount = new(big.Int).Sub(exchangerAmount, injectRewardAmount)
	db.AddBalance(beneficiaryExchanger, exchangerAmount)
	//db.AddBalance(InjectRewardAddress, injectRewardAmount)

	return nil
}

func AddExchangerToken(db vm.StateDB, address common.Address, amount *big.Int) {
	db.AddExchangerToken(address, amount)
}
func ModifyOpenExchangerTime(db vm.StateDB, address common.Address, blockNumber *big.Int) {
	db.ModifyOpenExchangerTime(address, blockNumber)
}
func SubExchangerToken(db vm.StateDB, address common.Address, amount *big.Int) {
	db.SubExchangerToken(address, amount)
}
func SubExchangerBalance(db vm.StateDB, address common.Address, amount *big.Int) {
	db.SubExchangerBalance(address, amount)
}

// VerifyExchangerBalance checks whether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func VerifyExchangerBalance(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	return db.GetExchangerBalance(addr).Cmp(amount) >= 0
}

func VoteOfficialNFT(db vm.StateDB, nominatedOfficialNFT *types.NominatedOfficialNFT, blocknumber *big.Int) error {
	return db.VoteOfficialNFT(nominatedOfficialNFT, blocknumber)
}

func ElectNominatedOfficialNFT(db vm.StateDB, blocknumber *big.Int) {
	db.ElectNominatedOfficialNFT(blocknumber)
}

func NextIndex(db vm.StateDB) *big.Int {
	return db.NextIndex()
}

func VoteOfficialNFTByApprovedExchanger(
	db vm.StateDB,
	blocknumber *big.Int,
	caller common.Address,
	to common.Address,
	wormholes *types.Wormholes,
	amount *big.Int) error {

	var number uint64 = 4096
	var royalty uint16 = 1000 // default 10%

	exchangerMsg := wormholes.ExchangerAuth.ExchangerOwner +
		wormholes.ExchangerAuth.To +
		wormholes.ExchangerAuth.BlockNumber

	originalExchanger, err := RecoverAddress(exchangerMsg, wormholes.ExchangerAuth.Sig)
	if err != nil {
		log.Error("VoteOfficialNFTByApprovedExchanger()", "Get buyer public key error", err)
		return ErrRecoverAddress
	}
	exchangerOwner := common.HexToAddress(wormholes.ExchangerAuth.ExchangerOwner)
	if originalExchanger != exchangerOwner {
		log.Error("VoteOfficialNFTByApprovedExchanger(), exchangerAuth",
			"wormholes.ExchangerAuth.ExchangerOwner", wormholes.ExchangerAuth.ExchangerOwner,
			"recovered exchanger ", originalExchanger)
		return ErrNotMatchAddress
	}

	//check if the exchanger_auth.to is same with sender,
	//return error if they are not same.
	approvedAddr := common.HexToAddress(wormholes.ExchangerAuth.To)
	if approvedAddr != caller {
		log.Error("BuyAndMintNFTByApprovedExchanger(), from of the tx is not approved!",
			"caller", caller.String(), "wormholes.ExchangerAuth.To", wormholes.ExchangerAuth.To)
		return errors.New("from of the tx is not approved!")
	}

	if !strings.HasPrefix(wormholes.ExchangerAuth.BlockNumber, "0x") &&
		!strings.HasPrefix(wormholes.ExchangerAuth.BlockNumber, "0X") {
		log.Error("BuyAndMintNFTByApprovedExchanger(), exchanger blocknumber format error",
			"wormholes.ExchangerAuth.BlockNumber", wormholes.ExchangerAuth.BlockNumber)
		return errors.New("exchanger blocknumber is not string of 0x!")
	}
	exchangerBlockNumber, ok := new(big.Int).SetString(wormholes.ExchangerAuth.BlockNumber[2:], 16)
	if !ok {
		log.Error("BuyAndMintNFTByApprovedExchanger(), exchanger blocknumber format error", "ok", ok)
		return errors.New("exchanger blocknumber is not string of 0x!")
	}
	if blocknumber.Cmp(exchangerBlockNumber) > 0 {
		log.Error("BuyAndMintNFTByApprovedExchanger(), exchanger's data is expired!",
			"exchangerBlockNumber", exchangerBlockNumber.Text(16), "blocknumber", blocknumber.Text(16))
		return errors.New("exchanger's data is expired!")
	}

	startIndex := db.NextIndex()
	var dir = wormholes.Dir
	if len(dir) <= 0 {
		dir = types.DefaultDir
	}
	var creator = wormholes.Creator
	if len(creator) <= 0 {
		creator = originalExchanger.Hex()
	}

	nominatedNFT := types.NominatedOfficialNFT{
		InjectedOfficialNFT: types.InjectedOfficialNFT{
			Dir:        dir,
			StartIndex: startIndex,
			//Number: wormholes.Number,
			Number: number,
			//Royalty: wormholes.Royalty,
			Royalty: royalty,
			Creator: creator,
			Address: originalExchanger,
		},
	}

	return db.VoteOfficialNFT(&nominatedNFT, blocknumber)
}

//func ChangeRewardFlag(db vm.StateDB, address common.Address, flag uint8) {
//	db.ChangeRewardFlag(address, flag)
//}

//func PledgeNFT(db vm.StateDB, nftaddress common.Address, blocknumber *big.Int) {
//	db.PledgeNFT(nftaddress, blocknumber)
//}
//
//func CancelPledgedNFT(db vm.StateDB, nftaddress common.Address) {
//	db.CancelPledgedNFT(nftaddress)
//}

func GetMergeNumber(db vm.StateDB, nftaddress common.Address) uint32 {
	return db.GetMergeNumber(nftaddress)
}

//func GetPledgedFlag(db vm.StateDB, nftaddress common.Address) bool {
//	return db.GetPledgedFlag(nftaddress)
//}
//
//func GetNFTPledgedBlockNumber(db vm.StateDB, nftaddress common.Address) *big.Int {
//	return db.GetNFTPledgedBlockNumber(nftaddress)
//}

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
		recoverAmount := new(big.Int).Mul(big.NewInt(int64(needRecoverCoe)), big.NewInt(100000000000000000))
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
