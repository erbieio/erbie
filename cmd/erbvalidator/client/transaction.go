package client

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/cmd/erbvalidator/tools"
	types2 "github.com/ethereum/go-ethereum/cmd/erbvalidator/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"log"
	"math/big"
	"strings"
)

const TranPrefix = "erbie:"

// NormalTransaction
//
//		Parameter Description
//	 to 			Account address
//	 value		transaction amount
//	 data
func (worm *Wormholes) NormalTransaction(to string, value int64, data string) (string, error) {
	ctx := context.Background()
	account, fromKey, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		log.Println("NormalTransaction() priKeyToAddress err ", err)
		return "", err
	}

	toAddr := common.HexToAddress(to)
	nonce, err := worm.PendingNonceAt(ctx, account)

	gasLimit := uint64(51000)
	gasPrice, err := worm.SuggestGasPrice(ctx)
	if err != nil {
		log.Println("NormalTransaction() suggestGasPrice err ", err)
		return "", err
	}

	wei, _ := new(big.Int).SetString("1000000000000000000", 10)
	charge := new(big.Int).Mul(big.NewInt(value), wei)
	tx := types.NewTransaction(nonce, toAddr, charge, gasLimit, gasPrice, []byte(data))
	chainID, err := worm.NetworkID(ctx)
	if err != nil {
		log.Println("NormalTransaction() networkID err=", err)
		return "", err
	}
	log.Println("chainID=", chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fromKey)
	if err != nil {
		log.Println("NormalTransaction() signTx err ", err)
		return "", err
	}
	err = worm.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Println("NormalTransaction() sendTransaction err ", err)
		return "", err
	}
	return strings.ToLower(signedTx.Hash().String()), nil
}

// Transfer CSBT transfer
//
//	Change ownership of CSBTs
//
//	Parameter Description
//	wormAddress: "0x8000000000000000000000000000000000000001",  worm address, the format is a decimal string, when it is SNFT, the length can be less than 42 (including 0x), representing the synthesized SNFT
//	to:         "0x814920c33b1a037F91a16B126282155c6F92A10F",  Target NFT user address
func (worm *Wormholes) Transfer(wormAddress, to string) (string, error) {
	err := tools.CheckHex("Transfer() wormAddress", wormAddress)
	if err != nil {
		return "", err
	}
	err = tools.CheckAddress("Transfer() to", to)
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	account, fromKey, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		return "", err
	}

	toAddr := common.HexToAddress(to)

	nonce, err := worm.PendingNonceAt(ctx, account)

	gasLimit := uint64(50000)
	gasPrice, err := worm.SuggestGasPrice(ctx)
	if err != nil {
		log.Println("Transfer() suggestGasPrice err ", err)
		return "", err
	}

	transaction := types2.Transaction{
		Type:        types2.Transfer,
		CSBTAddress: wormAddress,
		Version:     types2.WormHolesVersion,
	}

	data, err := json.Marshal(transaction)
	if err != nil {
		log.Println("Transfer() failed to format wormholes data")
		return "", err
	}

	tx_data := append([]byte(TranPrefix), data...)

	fmt.Println(string(tx_data))

	tx := types.NewTransaction(nonce, toAddr, big.NewInt(0), gasLimit, gasPrice, tx_data)
	chainID, err := worm.NetworkID(ctx)
	if err != nil {
		log.Println("Transfer() networkID err ", err)
		return "", err
	}
	log.Println("chainID=", chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fromKey)
	if err != nil {
		log.Println("Transfer() signTx err ", err)
		return "", err
	}
	err = worm.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Println("Transfer() sendTransaction err ", err)
		return "", err
	}
	return strings.ToLower(signedTx.Hash().String()), nil
}

// withdraw ERB to the owners of csbt
//
//	Parameter Description
//	wormAddress: "0x8000000000000000000000000000000000000001",  worm address, the format is a decimal string, when it is SNFT, the length can be less than 42 (including 0x), representing the synthesized SNFT
//	to:         "0x814920c33b1a037F91a16B126282155c6F92A10F",  Target NFT user address
func (worm *Wormholes) WithdrawERB(wormAddress, to string, value int64) (string, error) {
	err := tools.CheckHex("Transfer() wormAddress", wormAddress)
	if err != nil {
		return "", err
	}
	err = tools.CheckAddress("Transfer() to", to)
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	account, fromKey, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		return "", err
	}

	toAddr := common.HexToAddress(to)

	nonce, err := worm.PendingNonceAt(ctx, account)

	gasLimit := uint64(50000)
	gasPrice, err := worm.SuggestGasPrice(ctx)
	if err != nil {
		log.Println("Transfer() suggestGasPrice err ", err)
		return "", err
	}

	transaction := types2.Transaction{
		Type:        types2.Withdraw,
		CSBTAddress: wormAddress,
		Version:     types2.WormHolesVersion,
	}

	data, err := json.Marshal(transaction)
	if err != nil {
		log.Println("Transfer() failed to format wormholes data")
		return "", err
	}

	tx_data := append([]byte(TranPrefix), data...)

	fmt.Println(string(tx_data))

	wei, _ := new(big.Int).SetString("1000000000000000000", 10)
	amount := new(big.Int).Mul(big.NewInt(value), wei)
	tx := types.NewTransaction(nonce, toAddr, amount, gasLimit, gasPrice, tx_data)
	chainID, err := worm.NetworkID(ctx)
	if err != nil {
		log.Println("Transfer() networkID err ", err)
		return "", err
	}
	log.Println("chainID=", chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fromKey)
	if err != nil {
		log.Println("Transfer() signTx err ", err)
		return "", err
	}
	err = worm.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Println("Transfer() sendTransaction err ", err)
		return "", err
	}
	return strings.ToLower(signedTx.Hash().String()), nil
}

// TokenPledge
//
//	When a user wants to become a miner, he needs to do an ERB pledge transaction first to pledge the ERB needed to become a miner
func (worm *Wormholes) TokenPledge(to string, proxyAddress string, value int64) (string, error) {
	ctx := context.Background()
	account, fromKey, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		log.Println("TokenPledge() priKeyToAddress err ", err)
		return "", err
	}

	nonce, err := worm.PendingNonceAt(ctx, account)

	gasLimit := uint64(70000)
	gasPrice, err := worm.SuggestGasPrice(ctx)
	if err != nil {
		log.Println("TokenPledge() suggestGasPrice err ", err)
		return "", err
	}

	transaction := types2.Transaction{
		Type:         types2.TokenPledge,
		ProxyAddress: proxyAddress,
		Version:      types2.WormHolesVersion,
	}

	data, err := json.Marshal(transaction)
	if err != nil {
		log.Println("TokenPledge() failed to format wormholes data")
		return "", err
	}

	tx_data := append([]byte(TranPrefix), data...)
	fmt.Println(string(tx_data))

	toAddr := common.HexToAddress(to)
	wei, _ := new(big.Int).SetString("1000000000000000000", 10)
	pledge := new(big.Int).Mul(big.NewInt(value), wei)
	tx := types.NewTransaction(nonce, toAddr, pledge, gasLimit, gasPrice, tx_data)
	chainID, err := worm.NetworkID(ctx)
	if err != nil {
		log.Println("TokenPledge() networkID err=", err)
		return "", err
	}
	log.Println("chainID=", chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fromKey)
	if err != nil {
		log.Println("TokenPledge() signTx err ", err)
		return "", err
	}
	err = worm.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Println("TokenPledge() sendTransaction err ", err)
		return "", err
	}
	return strings.ToLower(signedTx.Hash().String()), nil
}

// TokenRevokesPledge
//
//	When the user does not want to be a miner, or no longer wants to pledge so much ERB, he can do ERB to revoke the pledge
func (worm *Wormholes) TokenRevokesPledge(to string, value int64) (string, error) {
	ctx := context.Background()
	account, fromKey, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		log.Println("TokenRevokesPledge() priKeyToAddress err ", err)
		return "", err
	}

	nonce, err := worm.PendingNonceAt(ctx, account)

	gasLimit := uint64(50000)
	gasPrice, err := worm.SuggestGasPrice(ctx)
	if err != nil {
		log.Println("TokenRevokesPledge() suggestGasPrice err ", err)
		return "", err
	}

	transaction := types2.Transaction{
		Type:    types2.TokenRevokesPledge,
		Version: types2.WormHolesVersion,
	}

	data, err := json.Marshal(transaction)
	if err != nil {
		log.Println("TokenRevokesPledge() failed to format wormholes data")
		return "", err
	}

	tx_data := append([]byte(TranPrefix), data...)
	fmt.Println(string(tx_data))

	toAddr := common.HexToAddress(to)
	wei, _ := new(big.Int).SetString("1000000000000000000", 10)
	pledge := new(big.Int).Mul(big.NewInt(value), wei)

	tx := types.NewTransaction(nonce, toAddr, pledge, gasLimit, gasPrice, tx_data)
	chainID, err := worm.NetworkID(ctx)
	if err != nil {
		log.Println("TokenRevokesPledge() networkID err=", err)
		return "", err
	}
	log.Println("chainID=", chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fromKey)
	if err != nil {
		log.Println("TokenRevokesPledge() signTx err ", err)
		return "", err
	}
	err = worm.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Println("TokenRevokesPledge() sendTransaction err ", err)
		return "", err
	}
	return strings.ToLower(signedTx.Hash().String()), nil
}

// Transfer CSBT transfer
//
//	Change ownership of CSBTs
//
//	Parameter Description
//	wormAddress: "0x8000000000000000000000000000000000000001",  worm address, the format is a decimal string, when it is SNFT, the length can be less than 42 (including 0x), representing the synthesized SNFT
//	to:         "0x814920c33b1a037F91a16B126282155c6F92A10F",  Target NFT user address
func (worm *Wormholes) RecoverCoefficient() (string, error) {

	ctx := context.Background()
	account, fromKey, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		return "", err
	}

	nonce, err := worm.PendingNonceAt(ctx, account)

	gasLimit := uint64(50000)
	gasPrice, err := worm.SuggestGasPrice(ctx)
	if err != nil {
		log.Println("Transfer() suggestGasPrice err ", err)
		return "", err
	}

	transaction := types2.Transaction{
		Type:    types2.RecoverCoefficient,
		Version: types2.WormHolesVersion,
	}

	data, err := json.Marshal(transaction)
	if err != nil {
		log.Println("Transfer() failed to format wormholes data")
		return "", err
	}

	tx_data := append([]byte(TranPrefix), data...)
	fmt.Println(string(tx_data))

	tx := types.NewTransaction(nonce, account, big.NewInt(0), gasLimit, gasPrice, tx_data)
	chainID, err := worm.NetworkID(ctx)
	if err != nil {

		log.Println("Transfer() networkID err ", err)
		return "", err
	}
	log.Println("chainID=", chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fromKey)
	if err != nil {
		log.Println("Transfer() signTx err ", err)
		return "", err
	}
	err = worm.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Println("Transfer() sendTransaction err ", err)
		return "", err
	}
	return strings.ToLower(signedTx.Hash().String()), nil
}

func (worm *Wormholes) GetAccount() common.Address {
	fmt.Println(worm.priKey)
	account, _, err := tools.PriKeyToAddress(worm.priKey)
	if err != nil {
		fmt.Println("GetAccount error : ", err)
		return common.Address{}
	}
	return account
}

//var _ APIs = &Wormholes{}
