package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestInjectedOfficialNFTList(t *testing.T) {
	injected1 := &InjectedOfficialNFT{
		Dir:        "/ipfs/test1111",
		StartIndex: new(big.Int).SetInt64(0),
		Number:     4096,
		Royalty:    100,
		Creator:    "0xB7987546EA03f4167e1F424C89C094BebbC112A6",
	}
	injected2 := &InjectedOfficialNFT{
		Dir:        "/ipfs/test2222",
		StartIndex: new(big.Int).SetInt64(4096),
		Number:     131072,
		Royalty:    100,
		Creator:    "0xB7987546EA03f4167e1F424C89C094BebbC112A6",
	}

	var injectedList InjectedOfficialNFTList
	injectedList.InjectedOfficialNFTs = append(injectedList.InjectedOfficialNFTs, injected1)
	injectedList.InjectedOfficialNFTs = append(injectedList.InjectedOfficialNFTs, injected2)

	address := common.HexToAddress("0x8000000000000000000000000000000000010000")

	injectedList.GetInjectedInfo(address)

}
