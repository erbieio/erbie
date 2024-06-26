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

package vm

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// emptyCodeHash is used by create to ensure deployment is disallowed to already
// deployed contract addresses (relevant after the account abstraction).
var emptyCodeHash = crypto.Keccak256Hash(nil)

const VALIDATOR_COEFFICIENT = 70

type (
	// CanTransferFunc is the signature of a transfer guard function
	CanTransferFunc func(StateDB, common.Address, *big.Int) bool
	// TransferFunc is the signature of a transfer function
	TransferFunc func(StateDB, common.Address, common.Address, *big.Int)
	// GetHashFunc returns the n'th block hash in the blockchain
	// and is used by the BLOCKHASH EVM op code.
	GetHashFunc func(uint64) common.Hash

	// VerifyCSBTOwnerFunc is to judge whether the owner own the csbt
	VerifyCSBTOwnerFunc func(StateDB, string, common.Address) bool
	// TransferCSBTFunc is the signature of a TransferCSBT function
	TransferCSBTFunc                          func(StateDB, string, common.Address, *big.Int) error
	ExchangeNFTToCurrencyFunc                 func(StateDB, common.Address, string, *big.Int) error
	PledgeTokenFunc                           func(StateDB, common.Address, *big.Int, *types.Wormholes, *big.Int) error
	StakerPledgeFunc                          func(StateDB, common.Address, common.Address, *big.Int, *big.Int, *types.Wormholes) error
	GetPledgedTimeFunc                        func(StateDB, common.Address, common.Address) *big.Int
	GetStakerPledgedFunc                      func(StateDB, common.Address, common.Address) *types.StakerExtension
	MinerConsignFunc                          func(StateDB, common.Address, *types.Wormholes) error
	MinerBecomeFunc                           func(StateDB, common.Address, common.Address) error
	IsExistOtherPledgedFunc                   func(StateDB, common.Address) bool
	ResetMinerBecomeFunc                      func(StateDB, common.Address) error
	CancelPledgedTokenFunc                    func(StateDB, common.Address, *big.Int)
	NewCancelStakerPledgeFunc                 func(StateDB, common.Address, common.Address, *big.Int, *big.Int) error
	GetNFTCreatorFunc                         func(StateDB, common.Address) common.Address
	IsExistNFTFunc                            func(StateDB, common.Address) bool
	VerifyPledgedBalanceFunc                  func(StateDB, common.Address, *big.Int) bool
	VerifyStakerPledgedBalanceFunc            func(StateDB, common.Address, common.Address, *big.Int) bool
	VerifyCancelValidatorPledgedBalanceFunc   func(StateDB, common.Address, *big.Int) bool
	GetNftAddressAndLevelFunc                 func(string) (common.Address, int, error)
	RecoverValidatorCoefficientFunc           func(StateDB, common.Address) error
	BatchForcedSaleSNFTByApproveExchangerFunc func(StateDB, *big.Int, common.Address, common.Address, *types.Wormholes, *big.Int) error
	GetDividendFunc                           func(StateDB, common.Address) error
	IsExistStakerStorageAddressFunc           func(StateDB, common.Address) bool
)

func (evm *EVM) precompile(addr common.Address) (PrecompiledContract, bool) {
	var precompiles map[common.Address]PrecompiledContract
	switch {
	case evm.chainRules.IsBerlin:
		precompiles = PrecompiledContractsBerlin
	case evm.chainRules.IsIstanbul:
		precompiles = PrecompiledContractsIstanbul
	case evm.chainRules.IsByzantium:
		precompiles = PrecompiledContractsByzantium
	default:
		precompiles = PrecompiledContractsHomestead
	}
	p, ok := precompiles[addr]
	return p, ok
}

// BlockContext provides the EVM with auxiliary information. Once provided
// it shouldn't be modified.
type BlockContext struct {
	// CanTransfer returns whether the account contains
	// sufficient ether to transfer the value
	CanTransfer CanTransferFunc
	// Transfer transfers ether from one account to the other
	Transfer TransferFunc
	// GetHash returns the hash corresponding to n
	GetHash GetHashFunc

	// *** modify to support nft transaction 20211215 begin ***
	// VerifyCSBTOwner is to judge whether the owner own the nft
	VerifyCSBTOwner VerifyCSBTOwnerFunc
	// TransferCSBT transfers NFT from one owner to the other
	TransferCSBT TransferCSBTFunc
	// *** modify to support nft transaction 20211215 end ***
	PledgeToken                         PledgeTokenFunc
	StakerPledge                        StakerPledgeFunc
	GetPledgedTime                      GetPledgedTimeFunc
	GetStakerPledged                    GetStakerPledgedFunc
	MinerConsign                        MinerConsignFunc
	MinerBecome                         MinerBecomeFunc
	IsExistOtherPledged                 IsExistOtherPledgedFunc
	ResetMinerBecome                    ResetMinerBecomeFunc
	CancelPledgedToken                  CancelPledgedTokenFunc
	NewCancelStakerPledge               NewCancelStakerPledgeFunc
	GetNFTCreator                       GetNFTCreatorFunc
	IsExistNFT                          IsExistNFTFunc
	VerifyPledgedBalance                VerifyPledgedBalanceFunc
	VerifyStakerPledgedBalance          VerifyStakerPledgedBalanceFunc
	VerifyCancelValidatorPledgedBalance VerifyCancelValidatorPledgedBalanceFunc
	GetNftAddressAndLevel               GetNftAddressAndLevelFunc
	RecoverValidatorCoefficient         RecoverValidatorCoefficientFunc
	IsExistStakerStorageAddress         IsExistStakerStorageAddressFunc
	// Block information

	ParentHeader *types.Header

	Coinbase    common.Address // Provides information for COINBASE
	GasLimit    uint64         // Provides information for GASLIMIT
	BlockNumber *big.Int       // Provides information for NUMBER
	Time        *big.Int       // Provides information for TIME
	Difficulty  *big.Int       // Provides information for DIFFICULTY
	BaseFee     *big.Int       // Provides information for BASEFEE
}

// TxContext provides the EVM with information about a transaction.
// All fields can change between transactions.
type TxContext struct {
	// Message information
	Origin   common.Address // Provides information for ORIGIN
	GasPrice *big.Int       // Provides information for GASPRICE
}

// EVM is the Ethereum Virtual Machine base object and provides
// the necessary tools to run a contract on the given state with
// the provided context. It should be noted that any error
// generated through any of the calls should be considered a
// revert-state-and-consume-all-gas operation, no checks on
// specific errors should ever be performed. The interpreter makes
// sure that any errors generated are to be considered faulty code.
//
// The EVM should never be reused and is not thread safe.
type EVM struct {
	// Context provides auxiliary blockchain related information
	Context BlockContext
	TxContext
	// StateDB gives access to the underlying state
	StateDB StateDB
	// Depth is the current call stack
	depth int

	// chainConfig contains information about the current chain
	chainConfig *params.ChainConfig
	// chain rules contains the chain rules for the current epoch
	chainRules params.Rules
	// virtual machine configuration options used to initialise the
	// evm.
	Config Config
	// global (to this context) ethereum virtual machine
	// used throughout the execution of the tx.
	interpreter *EVMInterpreter
	// abort is used to abort the EVM calling operations
	// NOTE: must be set atomically
	abort int32
	// callGasTemp holds the gas available for the current call. This is needed because the
	// available gas is calculated in gasCall* according to the 63/64 rule and later
	// applied in opCall*.
	callGasTemp uint64
}

// *** modify to support nft transaction 20211215 begin ***

// NewEVM returns a new EVM. The returned EVM is not thread safe and should
// only ever be used *once*.
func NewEVM(blockCtx BlockContext, txCtx TxContext, statedb StateDB, chainConfig *params.ChainConfig, config Config) *EVM {
	evm := &EVM{
		Context:     blockCtx,
		TxContext:   txCtx,
		StateDB:     statedb,
		Config:      config,
		chainConfig: chainConfig,
		chainRules:  chainConfig.Rules(blockCtx.BlockNumber),
	}
	evm.interpreter = NewEVMInterpreter(evm, config)
	return evm
}

// Reset resets the EVM with a new transaction context.Reset
// This is not threadsafe and should only be done very cautiously.
func (evm *EVM) Reset(txCtx TxContext, statedb StateDB) {
	evm.TxContext = txCtx
	evm.StateDB = statedb
}

// Cancel cancels any running EVM operation. This may be called concurrently and
// it's safe to be called multiple times.
func (evm *EVM) Cancel() {
	atomic.StoreInt32(&evm.abort, 1)
}

// Cancelled returns true if Cancel has been called
func (evm *EVM) Cancelled() bool {
	return atomic.LoadInt32(&evm.abort) == 1
}

// Interpreter returns the current interpreter
func (evm *EVM) Interpreter() *EVMInterpreter {
	return evm.interpreter
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

func GetCsbtAddrs(db StateDB, nftParentAddress string, addr common.Address) []common.Address {
	var nftAddrs []common.Address
	emptyAddress := common.Address{}
	if strings.HasPrefix(nftParentAddress, "0x") ||
		strings.HasPrefix(nftParentAddress, "0X") {
		nftParentAddress = string([]byte(nftParentAddress)[2:])
	}

	if len(nftParentAddress) != 39 {
		return nftAddrs
	}

	addrInt := big.NewInt(0)
	addrInt.SetString(nftParentAddress, 16)
	addrInt.Lsh(addrInt, 4)

	// 3. retrieve all the sibling leaf nodes of nftAddr
	siblingInt := big.NewInt(0)
	//nftAddrSLen := len(nftAddrS)
	for i := 0; i < 16; i++ {
		// 4. convert bigInt to common.Address, and then get Account from the trie.
		siblingInt.Add(addrInt, big.NewInt(int64(i)))
		//siblingAddr := common.BigToAddress(siblingInt)
		siblingAddrS := hex.EncodeToString(siblingInt.Bytes())
		siblingAddrSLen := len(siblingAddrS)
		var prefix0 string
		for i := 0; i < 40-siblingAddrSLen; i++ {
			prefix0 = prefix0 + "0"
		}
		siblingAddrS = prefix0 + siblingAddrS
		siblingAddr := common.HexToAddress(siblingAddrS)
		//fmt.Println("siblingAddr=", siblingAddr.String())

		siblingOwner := db.GetNFTOwner16(siblingAddr)
		if siblingOwner != emptyAddress &&
			siblingOwner != addr {
			nftAddrs = append(nftAddrs, siblingAddr)
		}
	}

	return nftAddrs
}

// Call executes the contract associated with the addr with the given input as
// parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
func (evm *EVM) Call(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	var nftTransaction bool = false
	var wormholes types.Wormholes
	if evm.Config.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	//fmt.Println("input=", string(input))
	//fmt.Println("caller.Address=", caller.Address().String())
	// *** modify to support nft transaction 20211215 begin ***
	if len(input) > types.TransactionTypeLen {
		if string(input[:types.TransactionTypeLen]) == types.TransactionType {
			jsonErr := json.Unmarshal(input[types.TransactionTypeLen:], &wormholes)
			if jsonErr == nil {
				nftTransaction = true
			} else {
				log.Error("EVM.Call(), wormholes unmarshal error", "jsonErr", jsonErr,
					"wormholes", string(input))
				return nil, gas, ErrWormholesFormat
			}
		}
	}

	// Fail if we're trying to transfer more than the available balance
	if nftTransaction {
		switch wormholes.Type {
		case 4:
			pledgedBalance := evm.StateDB.GetStakerPledgedBalance(caller.Address(), addr)
			if pledgedBalance.Cmp(value) != 0 {
				if value.Sign() > 0 && !evm.Context.VerifyStakerPledgedBalance(evm.StateDB, caller.Address(), addr, new(big.Int).Add(value, types.StakerBase())) {
					log.Error("Call()", "insufficient balance for transfer")
					return nil, gas, ErrInsufficientBalance
				}
			}

		default:
			if value.Sign() != 0 && !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
				return nil, gas, ErrInsufficientBalance
			}
		}
	} else {
		if value.Sign() != 0 && !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
			return nil, gas, ErrInsufficientBalance
		}
	}

	snapshot := evm.StateDB.Snapshot()
	p, isPrecompile := evm.precompile(addr)

	//if !evm.StateDB.Exist(addr) && !nftTransaction {
	if !evm.StateDB.Exist(addr) {
		//if !isPrecompile && evm.chainRules.IsEIP158 && value.Sign() == 0 {
		//	// Calling a non existing account, don't do anything, but ping the tracer
		//	if evm.Config.Debug && evm.depth == 0 {
		//		evm.Config.Tracer.CaptureStart(evm, caller.Address(), addr, false, input, gas, value)
		//		evm.Config.Tracer.CaptureEnd(ret, 0, 0, nil)
		//	}
		//	return nil, gas, nil
		//}
		evm.StateDB.CreateAccount(addr)
	}

	log.Info("EVM.Call()", "nftTransaction", nftTransaction)
	if nftTransaction {
		log.Info("EVM.Call()", "nftTransaction", nftTransaction, "wormholes.Type", wormholes.Type)
		ret, gas, err = evm.HandleCSBT(caller, addr, wormholes, gas, value)
		if err != nil {
			return ret, gas, err
		}
	} else {
		evm.Context.Transfer(evm.StateDB, caller.Address(), addr, value)
	}
	// *** modify to support nft transaction 20211215 end ***

	// Capture the tracer start/end events in debug mode
	if evm.Config.Debug && evm.depth == 0 {
		evm.Config.Tracer.CaptureStart(evm, caller.Address(), addr, false, input, gas, value)
		defer func(startGas uint64, startTime time.Time) { // Lazy evaluation of the parameters
			evm.Config.Tracer.CaptureEnd(ret, startGas-gas, time.Since(startTime), err)
		}(gas, time.Now())
	}

	if isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		code := evm.StateDB.GetCode(addr)
		if len(code) == 0 {
			ret, err = nil, nil // gas is unchanged
		} else {
			addrCopy := addr
			// If the account has no code, we can abort here
			// The depth-check is already done, and precompiles handled above
			contract := NewContract(caller, AccountRef(addrCopy), value, gas)
			contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), code)
			ret, err = evm.interpreter.Run(contract, input, false)
			gas = contract.Gas
		}
	}
	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, gas, err
}

func (evm *EVM) IsContractAddress(address common.Address) bool {
	emptyHash := common.Hash{}
	codeHash := evm.StateDB.GetCodeHash(address)
	if codeHash != emptyHash {
		return true
	}
	return false
}

func (evm *EVM) TransferNFTByContract(caller ContractRef, input []byte, gas uint64) (ret []byte, overGas uint64, err error) {
	prefix := "21eceff7"
	//strInput := "b88d4fde" +
	//	"000000000000000000000000d9145cce52d386f254917e481eb44e9943f39138" +
	//	"000000000000000000000000d9145cce52d386f254917e481eb44e9943f39138" +
	//	"0000000000000000000000000000000000000000000000000000000000000003" +
	//	"0000000000000000000000000000000000000000000000000000000000000008" +
	//	"0000000000000000000000000000000000000000000000000000000000000002" +
	//	"0102000000000000000000000000000000000000000000000000000000000000"
	strInput := hex.EncodeToString(input)
	constData1 := "0000000000000000000000000000000000000000000000000000000000000008"
	//0x21eceff70000000000000000000000005b38da6a701c568545dcfcb03fcb875f56beddc40000000000000000000000000000000000000000000000000000000000000001

	if len(input) != 138 {
		return nil, gas, errors.New("input len error")
	}
	if !strings.HasPrefix(strInput, prefix) {
		return nil, gas, errors.New("input data error")
	}
	fromBytes := input[16:36]
	toBytes := input[48:68]
	nftAddressBytes := input[80:100]

	data1 := input[100:132]
	data2 := input[132:164]
	//data3 := input[164:196]

	if hex.EncodeToString(data1) != constData1 {
		return nil, gas, errors.New("input format error")
	}
	bigData3Len, _ := new(big.Int).SetString(hex.EncodeToString(data2), 16)
	if bigData3Len.Uint64() > 32 {
		return nil, gas, errors.New("input data error")
	}

	from := common.BytesToAddress(fromBytes)
	to := common.BytesToAddress(toBytes)

	bigNftAddr := new(big.Int).SetBytes(nftAddressBytes)
	bigSnft, _ := new(big.Int).SetString("8000000000000000000000000000000000000", 16)
	var strNftAddress string
	strNftAddress = "0x"
	if bigNftAddr.Cmp(bigSnft) >= 0 { // snft
		strNftAddress = strNftAddress + bigNftAddr.Text(16)
	} else {
		strNftAddress = strNftAddress + hex.EncodeToString(nftAddressBytes)
	}
	//nftAddress := common.BytesToAddress(nftAddressBytes)

	if evm.Context.VerifyCSBTOwner(evm.StateDB, strNftAddress, from) {
		err := evm.Context.TransferCSBT(evm.StateDB, strNftAddress, to, evm.Context.BlockNumber)
		if err != nil {
			return nil, gas, err
		}
	}

	//if evm.IsContractAddress(to) {
	//	erc721 := OnERC721Received(caller.Address(), from, strNftAddress)
	//	ret, overGas, err = evm.Call(AccountRef(SnftVirtualContractAddress), to, erc721, gas, big.NewInt(0))
	//}

	return ret, overGas, err
}

// CallCode executes the contract associated with the addr with the given input
// as parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
//
// CallCode differs from Call in the sense that it executes the given address'
// code with the caller as context.
func (evm *EVM) CallCode(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	if evm.Config.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	// Note although it's noop to transfer X ether to caller itself. But
	// if caller doesn't have enough balance, it would be an error to allow
	// over-charging itself. So the check here is necessary.
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}
	var snapshot = evm.StateDB.Snapshot()

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		addrCopy := addr
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		contract := NewContract(caller, AccountRef(caller.Address()), value, gas)
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		ret, err = evm.interpreter.Run(contract, input, false)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// DelegateCall executes the contract associated with the addr with the given input
// as parameters. It reverses the state in case of an execution error.
//
// DelegateCall differs from CallCode in the sense that it executes the given address'
// code with the caller as context and the caller is set to the caller of the caller.
func (evm *EVM) DelegateCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if evm.Config.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	var snapshot = evm.StateDB.Snapshot()

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		addrCopy := addr
		// Initialise a new contract and make initialise the delegate values
		contract := NewContract(caller, AccountRef(caller.Address()), nil, gas).AsDelegate()
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		ret, err = evm.interpreter.Run(contract, input, false)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// StaticCall executes the contract associated with the addr with the given input
// as parameters while disallowing any modifications to the state during the call.
// Opcodes that attempt to perform such modifications will result in exceptions
// instead of performing the modifications.
func (evm *EVM) StaticCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if evm.Config.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// We take a snapshot here. This is a bit counter-intuitive, and could probably be skipped.
	// However, even a staticcall is considered a 'touch'. On mainnet, static calls were introduced
	// after all empty accounts were deleted, so this is not required. However, if we omit this,
	// then certain tests start failing; stRevertTest/RevertPrecompiledTouchExactOOG.json.
	// We could change this, but for now it's left for legacy reasons
	var snapshot = evm.StateDB.Snapshot()

	// We do an AddBalance of zero here, just in order to trigger a touch.
	// This doesn't matter on Mainnet, where all empties are gone at the time of Byzantium,
	// but is the correct thing to do and matters on other networks, in tests, and potential
	// future scenarios
	evm.StateDB.AddBalance(addr, big0)

	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		// At this point, we use a copy of address. If we don't, the go compiler will
		// leak the 'contract' to the outer scope, and make allocation for 'contract'
		// even if the actual execution ends on RunPrecompiled above.
		addrCopy := addr
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		contract := NewContract(caller, AccountRef(addrCopy), new(big.Int), gas)
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		// When an error was returned by the EVM or when setting the creation code
		// above we revert to the snapshot and consume any gas remaining. Additionally
		// when we're in Homestead this also counts for code storage gas errors.
		ret, err = evm.interpreter.Run(contract, input, true)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

type codeAndHash struct {
	code []byte
	hash common.Hash
}

func (c *codeAndHash) Hash() common.Hash {
	if c.hash == (common.Hash{}) {
		c.hash = crypto.Keccak256Hash(c.code)
	}
	return c.hash
}

// create creates a new contract using code as deployment code.
func (evm *EVM) create(caller ContractRef, codeAndHash *codeAndHash, gas uint64, value *big.Int, address common.Address) ([]byte, common.Address, uint64, error) {
	// Depth check execution. Fail if we're trying to execute above the
	// limit.
	if evm.depth > int(params.CallCreateDepth) {
		return nil, common.Address{}, gas, ErrDepth
	}
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, common.Address{}, gas, ErrInsufficientBalance
	}
	nonce := evm.StateDB.GetNonce(caller.Address())
	evm.StateDB.SetNonce(caller.Address(), nonce+1)
	// We add this to the access list _before_ taking a snapshot. Even if the creation fails,
	// the access-list change should not be rolled back
	if evm.chainRules.IsBerlin {
		evm.StateDB.AddAddressToAccessList(address)
	}
	// Ensure there's no existing contract already at the designated address
	contractHash := evm.StateDB.GetCodeHash(address)
	if evm.StateDB.GetNonce(address) != 0 || (contractHash != (common.Hash{}) && contractHash != emptyCodeHash) {
		return nil, common.Address{}, 0, ErrContractAddressCollision
	}
	// Create a new account on the state
	snapshot := evm.StateDB.Snapshot()
	evm.StateDB.CreateAccount(address)
	if evm.chainRules.IsEIP158 {
		evm.StateDB.SetNonce(address, 1)
	}
	evm.Context.Transfer(evm.StateDB, caller.Address(), address, value)

	// Initialise a new contract and set the code that is to be used by the EVM.
	// The contract is a scoped environment for this execution context only.
	contract := NewContract(caller, AccountRef(address), value, gas)
	contract.SetCodeOptionalHash(&address, codeAndHash)

	if evm.Config.NoRecursion && evm.depth > 0 {
		return nil, address, gas, nil
	}

	if evm.Config.Debug && evm.depth == 0 {
		evm.Config.Tracer.CaptureStart(evm, caller.Address(), address, true, codeAndHash.code, gas, value)
	}
	start := time.Now()

	ret, err := evm.interpreter.Run(contract, nil, false)

	// Check whether the max code size has been exceeded, assign err if the case.
	if err == nil && evm.chainRules.IsEIP158 && len(ret) > params.MaxCodeSize {
		err = ErrMaxCodeSizeExceeded
	}

	// Reject code starting with 0xEF if EIP-3541 is enabled.
	if err == nil && len(ret) >= 1 && ret[0] == 0xEF && evm.chainRules.IsLondon {
		err = ErrInvalidCode
	}

	// if the contract creation ran successfully and no errors were returned
	// calculate the gas required to store the code. If the code could not
	// be stored due to not enough gas set an error and let it be handled
	// by the error checking condition below.
	if err == nil {
		createDataGas := uint64(len(ret)) * params.CreateDataGas
		if contract.UseGas(createDataGas) {
			evm.StateDB.SetCode(address, ret)
		} else {
			err = ErrCodeStoreOutOfGas
		}
	}

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil && (evm.chainRules.IsHomestead || err != ErrCodeStoreOutOfGas) {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}

	if evm.Config.Debug && evm.depth == 0 {
		evm.Config.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
	}
	return ret, address, contract.Gas, err
}

// Create creates a new contract using code as deployment code.
func (evm *EVM) Create(caller ContractRef, code []byte, gas uint64, value *big.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {
	contractAddr = crypto.CreateAddress(caller.Address(), evm.StateDB.GetNonce(caller.Address()))
	return evm.create(caller, &codeAndHash{code: code}, gas, value, contractAddr)
}

// Create2 creates a new contract using code as deployment code.
//
// The different between Create2 with Create is Create2 uses sha3(0xff ++ msg.sender ++ salt ++ sha3(init_code))[12:]
// instead of the usual sender-and-nonce-hash as the address where the contract is initialized at.
func (evm *EVM) Create2(caller ContractRef, code []byte, gas uint64, endowment *big.Int, salt *uint256.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {
	codeAndHash := &codeAndHash{code: code}
	contractAddr = crypto.CreateAddress2(caller.Address(), salt.Bytes32(), codeAndHash.Hash().Bytes())
	return evm.create(caller, codeAndHash, gas, endowment, contractAddr)
}

// ChainConfig returns the environment's chain configuration
func (evm *EVM) ChainConfig() *params.ChainConfig { return evm.chainConfig }

func (evm *EVM) HandleCSBT(
	caller ContractRef,
	addr common.Address,
	wormholes types.Wormholes,
	gas uint64,
	value *big.Int) (ret []byte, leftOverGas uint64, err error) {

	formatErr := wormholes.CheckFormat()
	if formatErr != nil {
		log.Error("HandleCSBT() format error", "wormholes.Type", wormholes.Type, "error", formatErr, "blocknumber", evm.Context.BlockNumber.Uint64())
		return nil, gas, formatErr
	}

	switch wormholes.Type {
	case 1: //transfer csbt

		if evm.Context.VerifyCSBTOwner(evm.StateDB, wormholes.CSBTAddress, caller.Address()) {

			// whether csbt is first transfer
			if !evm.Context.IsExistStakerStorageAddress(evm.StateDB, caller.Address()) {
				log.Info("HandleCSBT(), TransferCSBT csbt not in Staker Storage >>>>>>>>>>", "wormholes.Type", wormholes.Type,
					"blocknumber", evm.Context.BlockNumber.Uint64())
				return nil, gas, ErrNotCreator
			}

			log.Info("HandleCSBT(), TransferCSBT>>>>>>>>>>", "wormholes.Type", wormholes.Type,
				"blocknumber", evm.Context.BlockNumber.Uint64())
			err := evm.Context.TransferCSBT(evm.StateDB, wormholes.CSBTAddress, addr, evm.Context.BlockNumber)
			if err != nil {
				log.Error("HandleCSBT(), TransferCSBT", "wormholes.Type", wormholes.Type,
					"error", err, "blocknumber", evm.Context.BlockNumber.Uint64())
				return nil, gas, err
			}
			log.Info("HandleCSBT(), TransferCSBT<<<<<<<<<<", "wormholes.Type", wormholes.Type,
				"blocknumber", evm.Context.BlockNumber.Uint64())
		} else {
			log.Error("HandleCSBT(), TransferCSBT", "wormholes.Type", wormholes.Type,
				"error", ErrNotOwner, "blocknumber", evm.Context.BlockNumber.Uint64())
			return nil, gas, ErrNotOwner
		}

	case 2:

		if evm.Context.VerifyCSBTOwner(evm.StateDB, wormholes.CSBTAddress, addr) {
			evm.Context.Transfer(evm.StateDB, caller.Address(), addr, value)
		} else {
			log.Error("HandleCSBT(), Withdraw ERB", "wormholes.Type", wormholes.Type,
				"error", ErrNotOwner, "blocknumber", evm.Context.BlockNumber.Uint64())
			return nil, gas, ErrNotOwner
		}

	case 3: //staker token

		stakerpledged := evm.Context.GetStakerPledged(evm.StateDB, caller.Address(), addr)
		if stakerpledged.Balance.Cmp(types.StakerBase()) < 0 {
			if value.Cmp(types.StakerBase()) < 0 {
				log.Error("HandleCSBT(), StakerPledge", "wormholes.Type", wormholes.Type,
					"error", ErrNotMoreThan100ERB, "blocknumber", evm.Context.BlockNumber.Uint64())
				return nil, gas, ErrNotMoreThan100ERB
			}
		}

		currentBlockNumber := new(big.Int).Set(evm.Context.BlockNumber)

		log.Info("HandleCSBT()", "StakerPledge.req", wormholes, "blocknumber", evm.Context.BlockNumber.Uint64())
		if evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
			log.Info("HandleCSBT(), StakerPledge>>>>>>>>>>", "wormholes.Type", wormholes.Type,
				"blocknumber", evm.Context.BlockNumber.Uint64())

			err := evm.Context.StakerPledge(evm.StateDB, caller.Address(), addr, value, currentBlockNumber, &wormholes)
			if err != nil {
				log.Error("HandleCSBT(), StakerPledge<<<<<<<<<<", "wormholes.Type", wormholes.Type, "error", err,
					"blocknumber", evm.Context.BlockNumber.Uint64())
				return nil, gas, err
			}

		} else {
			log.Error("HandleCSBT(), StakerPledge<<<<<<<<<<", "wormholes.Type", wormholes.Type,
				"error", ErrInsufficientBalance, "blocknumber", evm.Context.BlockNumber.Uint64())
			return nil, gas, ErrInsufficientBalance
		}

		err := evm.Context.ResetMinerBecome(evm.StateDB, addr)
		if err != nil {
			log.Error("HandleCSBT(), StakerPledge<<<<<<<<<<", "wormholes.Type", wormholes.Type,
				"blocknumber", evm.Context.BlockNumber.Uint64(), "err", err)
			return nil, gas, err
		}

		log.Info("HandleCSBT(), StakerPledge<<<<<<<<<<", "wormholes.Type", wormholes.Type,
			"blocknumber", evm.Context.BlockNumber.Uint64())

	case 4: // cancel pledge of token
		log.Info("HandleCSBT(), CancelPledgedToken>>>>>>>>>>", "wormholes.Type", wormholes.Type,
			"blocknumber", evm.Context.BlockNumber.Uint64())

		stakerpledged := evm.Context.GetStakerPledged(evm.StateDB, caller.Address(), addr)
		pledgedBalance := stakerpledged.Balance

		if pledgedBalance.Cmp(value) != 0 {
			if types.StakerBase().Cmp(new(big.Int).Sub(pledgedBalance, value)) > 0 {
				log.Error("HandleCSBT(), CancelPledgedToken", "wormholes.Type", wormholes.Type,
					"error", "the after revocation is less than 700ERB", "blocknumber", evm.Context.BlockNumber.Uint64())
				return nil, gas, errors.New("the after revocation is less than 700ERB")
			}
		}

		//if caller.Address() == addr {
		//	if !evm.Context.VerifyCancelValidatorPledgedBalance(evm.StateDB, addr, value) {
		//		log.Error("HandleCSBT(), CancelPledgedToken", "wormholes.Type", wormholes.Type,
		//			"the pledged amount is less than the pledged amount at other address", "blocknumber", evm.Context.BlockNumber.Uint64())
		//		return nil, gas, errors.New("the pledged amount is less than the pledged amount at other address")
		//	}
		//}

		if big.NewInt(types.CancelDayPledgedInterval).Cmp(new(big.Int).Sub(evm.Context.BlockNumber, stakerpledged.BlockNumber)) <= 0 {
			log.Info("HandleCSBT(), CancelPledgedToken, cancel all", "wormholes.Type", wormholes.Type,
				"blocknumber", evm.Context.BlockNumber.Uint64())

			err := evm.Context.NewCancelStakerPledge(evm.StateDB, caller.Address(), addr, value, evm.Context.BlockNumber)
			if err != nil {
				log.Error("HandleCSBT(), CancelPledgedToken", "wormholes.Type", wormholes.Type,
					"error", err, "blocknumber", evm.Context.BlockNumber.Uint64())
				return nil, gas, err
			}
		} else {
			log.Error("HandleCSBT(), CancelPledgedToken", "wormholes.Type", wormholes.Type,
				"error", ErrTooCloseToCancel, "blocknumber", evm.Context.BlockNumber.Uint64())
			return nil, gas, ErrTooCloseToCancel
		}

		log.Info("HandleCSBT(), CancelPledgedToken<<<<<<<<<<", "wormholes.Type", wormholes.Type,
			"blocknumber", evm.Context.BlockNumber.Uint64())

	case 5:
		log.Info("HandleCSBT(), RecoverValidatorCoefficient>>>>>>>>>>", "wormholes.Type", wormholes.Type,
			"blocknumber", evm.Context.BlockNumber.Uint64())
		err := evm.Context.RecoverValidatorCoefficient(evm.StateDB, caller.Address())
		if err != nil {
			return nil, gas, err
		}
		log.Info("HandleCSBT(), RecoverValidatorCoefficient<<<<<<<<<<", "wormholes.Type", wormholes.Type,
			"blocknumber", evm.Context.BlockNumber.Uint64())

	default:
		log.Error("HandleCSBT()", "wormholes.Type", wormholes.Type, "error", ErrNotExistNFTType,
			"blocknumber", evm.Context.BlockNumber.Uint64())
		return nil, gas, ErrNotExistNFTType
	}

	return nil, gas, nil
}

// IsOfficialNFT return true if nft address is created by official
func IsOfficialNFT(nftAddress common.Address) bool {
	maskByte := byte(128)
	nftByte := nftAddress[0]
	result := maskByte & nftByte
	if result == 128 {
		return true
	}
	return false
}

// UnstakingHeight @title    UnstakingHeight
// @description   UnstakingHeight Returns the height at which stakers can get their stake back
// @auth      mindcarver        2022/08/01
// @param     stakedAmt        *big.Int   the total amount of the current pledge
// @param     appendAmt        *big.Int   additional amount
// @param     sno        		uint64    starting height
// @param     cno       	    uint64    current height
// @param     lockedNo          uint64    lock time (counted in blocks)
// @return                      uint64     delay the amount of time (in blocks) that can be unstakes
func UnstakingHeight(stakedAmt, appendAmt *big.Int, sno, cno, lockedNo uint64) (uint64, error) {
	_, err := checkParams(stakedAmt, appendAmt, sno, cno)
	if err != nil {
		return 0, err
	}
	return unstakingHeight(stakedAmt, appendAmt, sno, cno, lockedNo), nil
}

// reference:https://github.com/wormholes-org/wormholes/issues/9
func unstakingHeight(stakedAmt *big.Int, appendAmt *big.Int, sno uint64, cno uint64, lockedNo uint64) uint64 {
	var curRemainingTime uint64

	if sno+lockedNo > cno {
		curRemainingTime = sno + lockedNo - cno
	}

	total := big.NewFloat(0).Add(new(big.Float).SetInt(stakedAmt), new(big.Float).SetInt(appendAmt))
	h1 := big.NewFloat(0).Mul(big.NewFloat(0).Quo(new(big.Float).SetInt(stakedAmt), total), new(big.Float).SetInt(big.NewInt(int64(curRemainingTime))))
	h2 := big.NewFloat(0).Mul(big.NewFloat(0).Quo(new(big.Float).SetInt(appendAmt), total), new(big.Float).SetInt(big.NewInt(int64(lockedNo))))
	delayHeight, _ := big.NewFloat(0).Add(h1, h2).Uint64()
	if delayHeight < lockedNo/2 {
		delayHeight = lockedNo / 2
	}
	return delayHeight
}

func checkParams(stakedAmt *big.Int, appendAmt *big.Int, sno uint64, cno uint64) (bool, error) {
	if stakedAmt.Cmp(big.NewInt(0)) == 0 || appendAmt.Cmp(big.NewInt(0)) == 0 {
		return false, errors.New("illegal amount")
	}
	if cno == 0 {
		return false, errors.New("illegal height")
	}
	if sno > cno {
		return false, errors.New("the current height is less than the starting height of the pledge")
	}
	return true, nil
}
