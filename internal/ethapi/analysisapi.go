package ethapi

import (
	"context"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"math/big"
)

func (s *PublicBlockChainAPI) GetRevocationValidators(ctx context.Context, start rpc.BlockNumber, end rpc.BlockNumber) ([]common.Address, error) {

	var lastValidators *types.ValidatorList
	var revocationAddrs []common.Address
	var isExist bool
	//for i := start; i <= end; i++ {
	//	statedb, _, err := s.b.StateAndHeaderByNumber(ctx, i)
	//	if err != nil {
	//		return nil
	//	}
	//	currentValidators := statedb.GetValidators(types.ValidatorStorageAddress)
	//	if lastValidators == nil {
	//		lastValidators = currentValidators
	//		continue
	//	}
	//
	//	for _, validator := range lastValidators.Validators {
	//
	//		if currentValidators.Exist(validator.Addr) {
	//			pledgedBalance := statedb.GetPledgedBalance(validator.Addr)
	//			if pledgedBalance.Cmp(big.NewInt(1)) > 0 {
	//				isExist = true
	//			}
	//		}
	//
	//		if !isExist {
	//			revocationAddrs = append(revocationAddrs, validator.Addr)
	//		}
	//      isExist = false
	//	}
	//
	//	lastValidators = currentValidators
	//}

	statedb, _, err := s.b.StateAndHeaderByNumber(ctx, start)
	if err != nil {
		return nil, err
	}
	currentValidators := statedb.GetValidators(types.ValidatorStorageAddress)
	if lastValidators == nil {
		lastValidators = currentValidators
	}
	statedb2, _, err := s.b.StateAndHeaderByNumber(ctx, end)
	if err != nil {
		return nil, err
	}
	currentValidators = statedb2.GetValidators(types.ValidatorStorageAddress)
	if lastValidators == nil {
		lastValidators = currentValidators
	}

	for _, validator := range lastValidators.Validators {

		if currentValidators.Exist(validator.Addr) {
			pledgedBalance := statedb.GetPledgedBalance(validator.Addr)
			if pledgedBalance.Cmp(big.NewInt(1)) > 0 {
				isExist = true
			}
		}

		if !isExist {
			revocationAddrs = append(revocationAddrs, validator.Addr)
		}

		isExist = false
	}

	return revocationAddrs, nil
}
