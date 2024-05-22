package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

// NFT and CSBT minting sequence storage address
var MintDeepStorageAddress = common.HexToAddress("0x0000000000000000000000000000000000000001")

// validators storage address
var ValidatorStorageAddress = common.HexToAddress("0x0000000000000000000000000000000000000002")

// stakers storage address
var StakerStorageAddress = common.HexToAddress("0x0000000000000000000000000000000000000003")

// Voting contract address
var VoteContractAddress = common.HexToAddress("0x0000000000000000000000000000000000000010")

// The amount of voting contract generated per block
var VoteAmountEachBlock, _ = new(big.Int).SetString("800000000000000000", 10)

// validator reward 0.54360 ERB
var DREBlockReward = big.NewInt(5.436e+17)

// The percentage of rewards that validator receives
// 10 represents 10 percent
var PercentageValidatorReward = 7

// Deflation rate
var DeflationRate = 0.85

// Deflation time of validator's reward
// reduce 15% block reward in per period
var ReduceRewardPeriod = uint64(365 * 720 * 24)

// Deflation time of staker's reward
var ExchangePeriod = uint64(6160) // 365 * 720 * 24 * 4 / 4096

// snft exchange price
var SNFTL0 = "30000000000000000"
var SNFTL1 = "60000000000000000"
var SNFTL2 = "180000000000000000"
var SNFTL3 = "1000000000000000000"

// Redemption staking time(one month)
var CancelDayPledgedInterval int64 = 720 * 24 * 30 // blockNumber of per hour * 24h * 30
// for test
// var CancelDayPledgedInterval int64 = 5 // blockNumber of per hour * 24h

// number of validators participating in consensus
var ConsensusValidatorsNum = 11

// Number of validators receiving rewards
var ValidatorRewardNum = 7

// The number of stakers receiving CSBT rewards
var StakerRewardNum = 4

// 一期包含的CSBT碎片数量
// CSBT版税
// 系统默认的CSBT的创建者
// The default location for storing metadata of CSBT in the system
var DefaultDir string = "/ipfs/Qmf3xw9rEmsjJdQTV3ZcyF4KfYGtxMkXdNQ8YkVqNmLHY8"

// The number of CSBT fragments included in the first phase
var DefaultNumber uint64 = 4096

// CSBT royalty
var DefaultRoyalty uint16 = 1000

// default creator
var DefaultCreator string = "0x0000000000000000000000000000000000000000"

// Parse the input data of the transaction according to TransactionType and TransactionTypeLen
// if TransactionType is erbie, the input data of the transaction is "type Wormholes struct".
var TransactionType = "erbie:"
var TransactionTypeLen = 6

func StakerBase() *big.Int {
	baseErb, _ := new(big.Int).SetString("1000000000000000000", 10)
	Erb100 := big.NewInt(350)
	Erb100.Mul(Erb100, baseErb)

	return Erb100
}

func ValidatorBase() *big.Int {
	baseErb, _ := new(big.Int).SetString("1000000000000000000", 10)
	Erb100000 := big.NewInt(35000)
	Erb100000.Mul(Erb100000, baseErb)

	return Erb100000
}
