package main

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/cmd/erbvalidator/client"
	"strings"
)

func Pledge(url string, validatorKey string, proxyKey string, value int64) (string, error) {

	if strings.HasPrefix(validatorKey, "0x") &&
		strings.HasPrefix(validatorKey, "0X") {
		validatorKey = validatorKey[2:]
	}
	if strings.HasPrefix(proxyKey, "0x") &&
		strings.HasPrefix(proxyKey, "0X") {
		proxyKey = proxyKey[2:]
	}
	if len(validatorKey) != 64 || len(proxyKey) != 64 {
		return "", errors.New("private key format error")
	}

	worm := client.NewClient(validatorKey, url)
	proxy := GetAccount(proxyKey)
	strProxy := proxy.Hex()

	validatorAddr := GetAccount(validatorKey)
	to := validatorAddr.Hex()

	hash := ""
	var err error
	if proxy == validatorAddr {
		hash, err = pledge(worm, to, "", value)
	} else {
		hash, err = pledge(worm, to, strProxy, value)
	}

	return hash, err
}

func pledge(worm *client.Wormholes, to string, proxy string, value int64) (string, error) {
	hash, err := worm.TokenPledge(to, proxy, value)
	if err != nil {
		fmt.Println("Pledge error : ", err)
	}
	return hash, err
}
