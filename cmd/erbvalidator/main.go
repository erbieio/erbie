package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	cmd := flag.Int("cmd", 0, "1: to be a validator.\n2: do not to be a validator.\n3: displays the corresponding address based on the private key")
	nodeUrl := flag.String("nodeurl", "http://127.0.0.1:8545", "external service url of the erbie node.")
	validatorKey := flag.String("prikey", "", "private key of account to be a validator.")
	proxyKey := flag.String("proxykey", "", "private key of proxy account.")
	value := flag.Int64("value", 350, "pledge amount of validator.")

	flag.Parse()
	if *cmd != 1 && *cmd != 2 && *cmd != 3 {
		fmt.Println("cmd must be a value of 1,2, 3")
		os.Exit(1)
	}

	h, err := ExecCmd(*cmd, *nodeUrl, *validatorKey, *proxyKey, *value)
	if err != nil {
		fmt.Println("hash", h, "Error ", err)
	}

}

func ExecCmd(cmd int, url string, validatorKey string, proxyKey string, value int64) (string, error) {
	var hash string
	var err error
	if cmd == 1 {
		hash, err = Pledge(url, validatorKey, proxyKey, value)
	} else if cmd == 2 {
		hash, err = UndoPledge(url, validatorKey, value)
	} else if cmd == 3 {
		if validatorKey != "" {
			if strings.HasPrefix(validatorKey, "0x") ||
				strings.HasPrefix(validatorKey, "0X") {
				validatorKey = validatorKey[2:]
			}
			validator := GetAccount(validatorKey)
			fmt.Println("validator address ", validator)
		}

		if proxyKey != "" {
			if strings.HasPrefix(proxyKey, "0x") ||
				strings.HasPrefix(proxyKey, "0X") {
				proxyKey = proxyKey[2:]
			}
			proxy := GetAccount(proxyKey)
			fmt.Println("proxy address ", proxy)
		}

	} else {
		fmt.Println("cmd must be a value of 1,2, 3")
		return "", errors.New("cmd must be a value of 1,2, 3")
	}
	return hash, err
}
