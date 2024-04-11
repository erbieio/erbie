package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type WormholesExtension struct {
	PledgedBalance     *big.Int
	PledgedBlockNumber *big.Int
	// *** modify to support nft transaction 20211215 ***
	//Owner common.Address
	// whether the account has a NFT exchanger
	ExchangerFlag    bool
	BlockNumber      *big.Int
	ExchangerBalance *big.Int
	//SNFTAgentRecipient common.Address
	VoteBlockNumber *big.Int
	VoteWeight      *big.Int
	Coefficient     uint8
	// The ratio that exchanger get.
	FeeRate       uint16
	ExchangerName string
	ExchangerURL  string
	// ApproveAddress have the right to handle all nfts of the account
	ApproveAddressList []common.Address

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
	newWorm.ExchangerFlag = worm.ExchangerFlag
	if worm.BlockNumber != nil {
		newWorm.BlockNumber = new(big.Int).Set(worm.BlockNumber)
	}
	if worm.ExchangerBalance != nil {
		newWorm.ExchangerBalance = new(big.Int).Set(worm.ExchangerBalance)
	}
	newWorm.ValidatorProxy = worm.ValidatorProxy
	if worm.VoteBlockNumber != nil {
		newWorm.VoteBlockNumber = new(big.Int).Set(worm.VoteBlockNumber)
	}
	if worm.VoteWeight != nil {
		newWorm.VoteWeight = new(big.Int).Set(worm.VoteWeight)
	}
	newWorm.Coefficient = worm.Coefficient
	newWorm.FeeRate = worm.FeeRate
	newWorm.ExchangerName = worm.ExchangerName
	newWorm.ExchangerURL = worm.ExchangerURL

	newWorm.ApproveAddressList = make([]common.Address, len(worm.ApproveAddressList))
	copy(newWorm.ApproveAddressList, worm.ApproveAddressList)
	newWorm.StakerExtension = *worm.StakerExtension.DeepCopy()
	newWorm.ValidatorExtension = *worm.ValidatorExtension.DeepCopy()

	return &newWorm
}

type AccountNFT struct {
	//Account
	Name                  string
	Symbol                string
	Owner                 common.Address
	NFTApproveAddressList common.Address

	// MergeLevel is the level of NFT merged
	MergeLevel  uint8
	MergeNumber uint32

	Creator   common.Address
	Royalty   uint16
	Exchanger common.Address
	MetaURL   string
}

func (nft *AccountNFT) DeepCopy() *AccountNFT {
	newNft := &AccountNFT{
		Name:                  nft.Name,
		Symbol:                nft.Symbol,
		Owner:                 nft.Owner,
		NFTApproveAddressList: nft.NFTApproveAddressList,
		MergeLevel:            nft.MergeLevel,
		MergeNumber:           nft.MergeNumber,
		Creator:               nft.Creator,
		Royalty:               nft.Royalty,
		Exchanger:             nft.Exchanger,
		MetaURL:               nft.MetaURL,
	}

	return newNft
}

type AccountStaker struct {
	Mint          MintDeep
	Validators    ValidatorList
	Stakers       StakerList
	Snfts         InjectedOfficialNFTList
	Nominee       *NominatedOfficialNFT `rlp:"nil"`
	SNFTL3Addrs   []common.Address
	DividendAddrs []common.Address
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
	newStaker.Snfts = *staker.Snfts.DeepCopy()

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

	newStaker.SNFTL3Addrs = append(newStaker.SNFTL3Addrs, staker.SNFTL3Addrs...)
	newStaker.DividendAddrs = append(newStaker.DividendAddrs, staker.DividendAddrs...)

	return &newStaker
}
