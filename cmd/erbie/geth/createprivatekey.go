package geth

import (
	"fmt"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func CreatePrivateKey() error {
	// generate random.
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		utils.Fatalf("Failed to generate random private key: %v", err)
		return err
	}
	hexPrivateKey := hexutil.Encode(crypto.FromECDSA(privateKey))
	account := crypto.PubkeyToAddress(privateKey.PublicKey).String()
	fmt.Println("Warning!!!: It is strongly recommended to use this feature locally and not on a remote server, " +
		"as there is a risk of exposing private keys on remote servers.")
	fmt.Println("new address : ", account)
	fmt.Println("private key : ", hexPrivateKey)

	return nil
}
