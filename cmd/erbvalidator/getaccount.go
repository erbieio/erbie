package main

import (
	"fmt"
	"github.com/ethereum/go-ethereum/cmd/erbvalidator/tools"
	"github.com/ethereum/go-ethereum/common"
)

func GetAccount(key string) common.Address {
	account, _, err := tools.PriKeyToAddress(key)
	if err != nil {
		fmt.Println("GetAccount error : ", err)
		return common.Address{}
	}
	//fmt.Println("account : ", account.Hex())
	return account
}
