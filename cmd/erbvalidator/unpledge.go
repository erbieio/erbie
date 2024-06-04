package main

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/cmd/erbvalidator/client"
	"strings"
)

func UndoPledge(url string, validatorKey string, value int64) (string, error) {
	if strings.HasPrefix(validatorKey, "0x") &&
		strings.HasPrefix(validatorKey, "0X") {
		validatorKey = validatorKey[2:]
	}
	if len(validatorKey) != 64 {
		return "", errors.New("private key format error")
	}

	worm := client.NewClient(validatorKey, url)

	validatorAddr := GetAccount(validatorKey)
	to := validatorAddr.Hex()

	hash, err := undoPledge(worm, to, value)

	return hash, err
}

func undoPledge(worm *client.Wormholes, to string, value int64) (string, error) {
	hash, err := worm.TokenRevokesPledge(to, value)
	if err != nil {
		fmt.Println("UndoPledge error : ", err)
	}

	return hash, err
}
