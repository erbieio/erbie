package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type WormholesExtension struct {
	PledgedBalance     *big.Int
	PledgedBlockNumber *big.Int
	VoteBlockNumber    *big.Int
	VoteWeight         *big.Int
	Coefficient        uint8

	StakerExtension    StakersExtensionList
	ValidatorExtension ValidatorsExtensionList
	ValidatorProxy     common.Address
}

func (worm *WormholesExtension) DeepCopy() *WormholesExtension {
	var newWorm WormholesExtension

	if worm.PledgedBalance != nil {
		newWorm.PledgedBalance = new(big.Int).Set(worm.PledgedBalance)
	}
	if worm.PledgedBlockNumber != nil {
		newWorm.PledgedBlockNumber = new(big.Int).Set(worm.PledgedBlockNumber)
	}

	newWorm.ValidatorProxy = worm.ValidatorProxy
	if worm.VoteBlockNumber != nil {
		newWorm.VoteBlockNumber = new(big.Int).Set(worm.VoteBlockNumber)
	}
	if worm.VoteWeight != nil {
		newWorm.VoteWeight = new(big.Int).Set(worm.VoteWeight)
	}
	newWorm.Coefficient = worm.Coefficient

	newWorm.StakerExtension = *worm.StakerExtension.DeepCopy()
	newWorm.ValidatorExtension = *worm.ValidatorExtension.DeepCopy()

	return &newWorm
}

type AccountNFT struct {
	//Account
	Name    string
	Symbol  string
	Owner   common.Address
	Creator common.Address
	MetaURL string
}

func (nft *AccountNFT) DeepCopy() *AccountNFT {
	newNft := &AccountNFT{
		Name:    nft.Name,
		Symbol:  nft.Symbol,
		Owner:   nft.Owner,
		Creator: nft.Creator,
		MetaURL: nft.MetaURL,
	}

	return newNft
}

type AccountStaker struct {
	Mint       MintDeep
	Validators ValidatorList
	Stakers    StakerList
	Csbts      InjectedOfficialNFTList
	Nominee    *NominatedOfficialNFT `rlp:"nil"`
}

func (staker *AccountStaker) DeepCopy() *AccountStaker {
	var newStaker AccountStaker

	if staker.Mint.OfficialMint != nil {
		newStaker.Mint.OfficialMint = new(big.Int).Set(staker.Mint.OfficialMint)
	}
	if staker.Mint.UserMint != nil {
		newStaker.Mint.UserMint = new(big.Int).Set(staker.Mint.UserMint)
	}

	newStaker.Validators = *staker.Validators.DeepCopy()
	newStaker.Stakers = *staker.Stakers.DeepCopy()
	newStaker.Csbts = *staker.Csbts.DeepCopy()

	if staker.Nominee != nil {
		nominee := &NominatedOfficialNFT{}

		nominee.Dir = staker.Nominee.Dir
		nominee.StartIndex = new(big.Int).Set(staker.Nominee.StartIndex)
		nominee.Number = staker.Nominee.Number
		nominee.Royalty = staker.Nominee.Royalty
		nominee.Creator = staker.Nominee.Creator
		nominee.Address = staker.Nominee.Address
		nominee.VoteWeight = new(big.Int).Set(staker.Nominee.VoteWeight)

		newStaker.Nominee = nominee
	}

	return &newStaker
}
