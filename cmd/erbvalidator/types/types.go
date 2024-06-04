package types

import "github.com/ethereum/go-ethereum/common"

const WormHolesVersion = "v0.0.1"

const (
	Transfer = iota + 1
	Withdraw
	TokenPledge
	TokenRevokesPledge
	RecoverCoefficient
)

// Transaction struct for handling NFT transactions
type Transaction struct {
	Type         uint8  `json:"type"`
	CSBTAddress  string `json:"csbt_address,omitempty"`
	ProxyAddress string `json:"proxy_address,omitempty"`
	ProxySign    string `json:"proxy_sign,omitempty"`
	Creator      string `json:"creator,omitempty"`
	Version      string `json:"version,omitempty"`
}

type Buyauth struct {
	Exchanger   string `json:"exchanger,omitempty"`
	BlockNumber string `json:"block_number,omitempty"`
	Sig         string `json:"sig,omitempty"`
}

type Sellerauth struct {
	Exchanger   string `json:"exchanger,omitempty"`
	BlockNumber string `json:"block_number,omitempty"`
	Sig         string `json:"sig,omitempty"`
}

type Buyer struct {
	Amount      string `json:"price,omitempty"`
	NFTAddress  string `json:"nft_address,omitempty"`
	Exchanger   string `json:"exchanger,omitempty"`
	BlockNumber string `json:"block_number,omitempty"`
	Seller      string `json:"seller,omitempty"`
	Sig         string `json:"sig,omitempty"`
}

type Seller1 struct {
	Amount      string `json:"price,omitempty"`
	NFTAddress  string `json:"nft_address,omitempty"`
	Exchanger   string `json:"exchanger,omitempty"`
	BlockNumber string `json:"block_number,omitempty"`
	Sig         string `json:"sig,omitempty"`
}

type Seller2 struct {
	Amount        string `json:"price,omitempty"`
	Royalty       string `json:"royalty,omitempty"`
	MetaURL       string `json:"meta_url,omitempty"`
	ExclusiveFlag string `json:"exclusive_flag,omitempty"`
	Exchanger     string `json:"exchanger,omitempty"`
	BlockNumber   string `json:"block_number,omitempty"`
	Sig           string `json:"sig,omitempty"`
}

type ExchangerAuth struct {
	ExchangerOwner string `json:"exchanger_owner,omitempty"`
	To             string `json:"to,omitempty"`
	BlockNumber    string `json:"block_number,omitempty"`
	Sig            string `json:"sig,omitempty"`
}

type BlockParticipants struct {
	Address     common.Address `json:"address"`
	Coefficient uint8          `json:"coefficient"`
}
