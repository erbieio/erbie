package main

import (
	"github.com/ethereum/go-ethereum/cmd/erbie/geth"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/sgiccommon"
	"os"
	"syscall"
)

func main() {

	if len(os.Args) == 2 && os.Args[1] == "newkey" {
		geth.CreatePrivateKey()
		return
	}

	// change "--mainnet" to "--publicnet"
	changeArgs()

	//sigs := make(chan os.Signal, 1)
	stopWormhles := make(chan struct{})
	//done := make(chan bool, 1)
	//signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go geth.GethRun(stopWormhles)
	//go ipfs.IpfsRun(stopWormhles)
	//go nftserver.NftServerRun(stopWormhles)

	for {
		select {
		//case <- sigs:
		//	os.Exit(1)
		case <-stopWormhles:
			os.Exit(2)
		case <-sgiccommon.Sigc:
			utils.Sigc <- syscall.SIGTERM
		}
	}

}

func changeArgs() {
	for k, arg := range os.Args {
		if isMainNet(arg) {
			os.Args[k] = "--publicnet"
		}
	}
}

func isMainNet(arg string) bool {
	if arg == "--mainnet" {
		return true
	}

	return false
}
