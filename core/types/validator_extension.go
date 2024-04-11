package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

// Pledge a list of account addresses on the validator
// ValidatorsExtensionList is staker list
type ValidatorsExtensionList struct {
	ValidatorExtensions []*ValidatorExtension
}

type ValidatorExtension struct {
	Addr        common.Address
	Balance     *big.Int
	BlockNumber *big.Int
}

func (vl *ValidatorsExtensionList) AddValidatorPledge(addr common.Address, balance *big.Int, blocknumber *big.Int) bool {
	for _, v := range vl.ValidatorExtensions {
		if v.Addr == addr {
			v.Balance.Add(v.Balance, balance)
			v.BlockNumber = blocknumber
			return true
		}
	}
	vl.ValidatorExtensions = append(vl.ValidatorExtensions, &ValidatorExtension{Addr: addr, Balance: balance, BlockNumber: blocknumber})
	return true
}

func (vl *ValidatorsExtensionList) RemoveValidatorPledge(addr common.Address, balance *big.Int) bool {
	for i, v := range vl.ValidatorExtensions {
		if v.Addr == addr {
			if v.Balance.Cmp(balance) > 0 {
				v.Balance.Sub(v.Balance, balance)
				return true
			} else {
				v.Balance.Sub(v.Balance, balance)
				vl.ValidatorExtensions = append(vl.ValidatorExtensions[:i], vl.ValidatorExtensions[i+1:]...)
				return true
			}
		}
	}
	return false
}

func (vl *ValidatorsExtensionList) CancelValidatorPledge(addr common.Address) bool {
	for i, v := range vl.ValidatorExtensions {
		if v.Addr == addr {
			vl.ValidatorExtensions = append(vl.ValidatorExtensions[:i], vl.ValidatorExtensions[i+1:]...)
			return true
		}
	}
	return false
}

func NewValidatorPledge(addr common.Address, balance *big.Int, blocknumber *big.Int) *ValidatorExtension {
	return &ValidatorExtension{Addr: addr, Balance: balance, BlockNumber: blocknumber}
}

func (vl *ValidatorsExtensionList) DeepCopy() *ValidatorsExtensionList {
	tempValidatorList := &ValidatorsExtensionList{
		ValidatorExtensions: make([]*ValidatorExtension, 0, len(vl.ValidatorExtensions)),
	}
	for _, staker := range vl.ValidatorExtensions {
		tempStaker := ValidatorExtension{
			Addr:        staker.Addr,
			Balance:     new(big.Int).Set(staker.Balance),
			BlockNumber: new(big.Int).Set(staker.BlockNumber),
		}
		tempValidatorList.ValidatorExtensions = append(tempValidatorList.ValidatorExtensions, &tempStaker)
	}
	return tempValidatorList
}

func (vl *ValidatorsExtensionList) IsExist(addr common.Address) bool {
	if vl == nil ||
		len(vl.ValidatorExtensions) == 0 {
		return false
	}

	for _, staker := range vl.ValidatorExtensions {
		if staker.Addr == addr {
			return true
		}
	}

	return false
}

func (vl *ValidatorsExtensionList) GetBalance(addr common.Address) *big.Int {
	if vl == nil || vl.ValidatorExtensions == nil {
		return big.NewInt(0)
	}

	for _, staker := range vl.ValidatorExtensions {
		if staker.Addr == addr {
			return staker.Balance
		}
	}

	return big.NewInt(0)
}

func (vl *ValidatorsExtensionList) GetLen() int {
	if vl == nil || vl.ValidatorExtensions == nil {
		return 0
	}

	return len(vl.ValidatorExtensions)
}

func (vl *ValidatorsExtensionList) GetAllBalance() *big.Int {
	totalBalance := big.NewInt(0)
	if vl == nil || vl.ValidatorExtensions == nil {
		return totalBalance
	}

	for _, staker := range vl.ValidatorExtensions {
		totalBalance.Add(totalBalance, staker.Balance)
	}

	return totalBalance
}
