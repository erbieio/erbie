package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

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

func (worm *WormholesExtension) DeepCopy() *WormholesExtension {
	var newWorm WormholesExtension

	if worm.PledgedBalance != nil {
		newWorm.PledgedBalance = new(big.Int).Set(worm.PledgedBalance)
	}
	if worm.PledgedBlockNumber != nil {
		newWorm.PledgedBlockNumber = new(big.Int).Set(worm.PledgedBlockNumber)
	}

	newWorm.ValidatorProxy = worm.ValidatorProxy
	newWorm.Coefficient = worm.Coefficient

	newWorm.StakerExtension = *worm.StakerExtension.DeepCopy()
	newWorm.ValidatorExtension = *worm.ValidatorExtension.DeepCopy()

	return &newWorm
}

type AccountCSBT struct {
	Owner   common.Address
	Creator common.Address
}

func (csbt *AccountCSBT) DeepCopy() *AccountCSBT {
	newCsbt := &AccountCSBT{
		Owner:   csbt.Owner,
		Creator: csbt.Creator,
	}

	return newCsbt
}

type AccountStaker struct {
	Mint         MintDeep
	Validators   ValidatorList
	CSBTCreators StakerList
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
	newStaker.CSBTCreators = *staker.CSBTCreators.DeepCopy()

	return &newStaker
}
