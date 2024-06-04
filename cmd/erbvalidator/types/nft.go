package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type Account struct {
	Nonce    uint64
	Balance  *big.Int
	Root     common.Hash // merkle root of the storage trie
	CodeHash []byte
	Worm     *WormholesExtension `rlp:"nil"`
	Csbt     *AccountCSBT        `rlp:"nil"`
	Staker   *AccountStaker      `rlp:"nil"`
	Extra    []byte
}

type WormholesExtension struct {
	// validator's sum pledged balance
	PledgedBalance *big.Int
	// the last blocknumber that staker pledged
	PledgedBlockNumber *big.Int
	Coefficient        uint8

	StakerExtension    StakersExtensionList
	ValidatorExtension ValidatorsExtensionList
	ValidatorProxy     common.Address
}

type StakersExtensionList struct {
	StakerExtensions []*StakerExtension
}
type StakerExtension struct {
	Addr        common.Address
	Balance     *big.Int
	BlockNumber *big.Int
}

type ValidatorsExtensionList struct {
	ValidatorExtensions []*ValidatorExtension
}

type ValidatorExtension struct {
	Addr        common.Address
	Balance     *big.Int
	BlockNumber *big.Int
}

type AccountCSBT struct {
	Owner   common.Address
	Creator common.Address
}

type AccountStaker struct {
	Mint         MintDeep
	Validators   ValidatorList
	CSBTCreators StakerList
}

type MintDeep struct {
	UserMint     *big.Int
	OfficialMint *big.Int
	//ExchangeList SNFTExchangeList
}

type StakerList struct {
	Stakers []*Staker
}

type Staker struct {
	Addr    common.Address
	Balance *big.Int
}

func (s *Staker) Address() common.Address {
	return s.Addr
}

func (sl *StakerList) IsExist(addr common.Address) bool {
	for _, v := range sl.Stakers {
		if v.Address() == addr {
			return true
		}
	}
	return false
}

type InjectedOfficialNFT struct {
	Dir        string         `json:"dir"`
	StartIndex *big.Int       `json:"start_index"`
	Number     uint64         `json:"number"`
	Royalty    uint16         `json:"royalty"`
	Creator    string         `json:"creator"`
	Address    common.Address `json:"address"`
	VoteWeight *big.Int       `json:"vote_weight"`
}

type InjectedOfficialNFTList struct {
	InjectedOfficialNFTs []*InjectedOfficialNFT
}

type NominatedOfficialNFT struct {
	InjectedOfficialNFT
}

type ValidatorList struct {
	Validators []*Validator
}

type Validator struct {
	Addr    common.Address
	Balance *big.Int
	Proxy   common.Address
	Weight  []*big.Int
}

func (vl *ValidatorList) Exist(addr common.Address) bool {
	for _, v := range vl.Validators {
		if v.Addr == addr || v.Proxy == addr {
			return true
		}
	}
	return false
}

func (vl *ValidatorList) GetValidator(addr common.Address) *Validator {
	for _, v := range vl.Validators {
		if v.Addr == addr {
			return v
		}
	}
	return nil
}

type BeneficiaryAddress struct {
	Address      common.Address
	NftAddress   common.Address
	RewardAmount *big.Int
}

type BeneficiaryAddressList []*BeneficiaryAddress

type ActiveMiner struct {
	Address common.Address
	Balance *big.Int
	Height  uint64
}

type ActiveMinerList struct {
	ActiveMiners []*ActiveMiner
}

type MinerProxy struct {
	Address common.Address
	Proxy   common.Address
}

type MinerProxyList []*MinerProxy
