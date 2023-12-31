// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package geth

import (
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

// Genesis block for nodes which don't care about the DAO fork (i.e. not configured)
var daoOldGenesis = `{
	"alloc"      : {},
	"coinbase"   : "0x0000000000000000000000000000000000000000",
	"difficulty" : "0x20000",
	"extraData"  : "",
	"gasLimit"   : "0x2fefd8",
	"nonce"      : "0x0000000000000042",
	"mixhash"    : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"parentHash" : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"timestamp"  : "0x00",
	"config"     : {
		"homesteadBlock" : 0
	},
 "alloc": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc00000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda1000000"
    }
  },
  "stake": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda10000"
    }
  },
  "validator": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda10000"
    }
  },
  "royalty":100,
  "creator":      "0x35636d53Ac3DfF2b2347dDfa37daD7077b3f5b6F",
  "inject_number": 4096,
  "start_index":   0,
  "dir":          "/ipfs/QmS2U6Mu2X5HaUbrbVp6JoLmdcFphXiD98avZnq1My8vef"
}`

// Genesis block for nodes which actively oppose the DAO fork
var daoNoForkGenesis = `{
	"alloc"      : {},
	"coinbase"   : "0x0000000000000000000000000000000000000000",
	"difficulty" : "0x20000",
	"extraData"  : "",
	"gasLimit"   : "0x2fefd8",
	"nonce"      : "0x0000000000000042",
	"mixhash"    : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"parentHash" : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"timestamp"  : "0x00",
	"config"     : {
		"homesteadBlock" : 0,
		"daoForkBlock"   : 314,
		"daoForkSupport" : false
	},
 "alloc": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc00000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda1000000"
    }
  },
  "stake": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda10000"
    }
  },
  "validator": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda10000"
    }
  },
  "royalty":100,
  "creator":      "0x35636d53Ac3DfF2b2347dDfa37daD7077b3f5b6F",
  "inject_number": 4096,
  "start_index":   0,
  "dir":          "/ipfs/QmS2U6Mu2X5HaUbrbVp6JoLmdcFphXiD98avZnq1My8vef"
}`

// Genesis block for nodes which actively support the DAO fork
var daoProForkGenesis = `{
	"alloc"      : {},
	"coinbase"   : "0x0000000000000000000000000000000000000000",
	"difficulty" : "0x20000",
	"extraData"  : "",
	"gasLimit"   : "0x2fefd8",
	"nonce"      : "0x0000000000000042",
	"mixhash"    : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"parentHash" : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"timestamp"  : "0x00",
	"config"     : {
		"homesteadBlock" : 0,
		"daoForkBlock"   : 314,
		"daoForkSupport" : true
	},
 "alloc": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc00000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda1000000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc00000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda1000000"
    }
  },
  "stake": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda10000"
    }
  },
  "validator": {
    "0x091DBBa95B26793515cc9aCB9bEb5124c479f27F": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x107837Ea83f8f06533DDd3fC39451Cd0AA8DA8BD": {
      "balance": "0xed2b525841adfc000"
    },
    "0x612DFa56DcA1F581Ed34b9c60Da86f1268Ab6349": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0x84d84e6073A06B6e784241a9B13aA824AB455326": {
      "balance": "0xed2b525841adfc000"
    },
    "0x9e4d5C72569465270232ed7Af71981Ee82d08dBF": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xa270bBDFf450EbbC2d0413026De5545864a1b6d6": {
      "balance": "0xed2b525841adfc000"
    },
    "0x4110E56ED25e21267FBeEf79244f47ada4e2E963": {
      "balance": "0xd3c21bcecceda10000"
    },
    "0xdb33217fE3F74bD41c550B06B624E23ab7f55d05": {
      "balance": "0xed2b525841adfc000"
    },
    "0xE2FA892CC5CC268a0cC1d924EC907C796351C645": {
      "balance": "0xd3c21bcecceda10000"
    }
  },
  "royalty":100,
  "creator":      "0x35636d53Ac3DfF2b2347dDfa37daD7077b3f5b6F",
  "inject_number": 4096,
  "start_index":   0,
  "dir":          "/ipfs/QmS2U6Mu2X5HaUbrbVp6JoLmdcFphXiD98avZnq1My8vef"
}`

var daoGenesisHash = common.HexToHash("5e1fc79cb4ffa4739177b5408045cd5d51c6cf766133f23f7cd72ee1f8d790e0")
var daoGenesisForkBlock = big.NewInt(314)

// TestDAOForkBlockNewChain tests that the DAO hard-fork number and the nodes support/opposition is correctly
// set in the database after various initialization procedures and invocations.
func TestDAOForkBlockNewChain(t *testing.T) {
	for i, arg := range []struct {
		genesis     string
		expectBlock *big.Int
		expectVote  bool
	}{
		// Test DAO Default Mainnet
		//{"", params.MainnetChainConfig.DAOForkBlock, true},
		// test DAO Init Old Privnet
		{daoOldGenesis, nil, false},
		// test DAO Default No Fork Privnet
		{daoNoForkGenesis, daoGenesisForkBlock, false},
		// test DAO Default Pro Fork Privnet
		{daoProForkGenesis, daoGenesisForkBlock, true},
	} {
		testDAOForkBlockNewChain(t, i, arg.genesis, arg.expectBlock, arg.expectVote)
	}
}

func testDAOForkBlockNewChain(t *testing.T, test int, genesis string, expectBlock *big.Int, expectVote bool) {
	// Create a temporary data directory to use and inspect later
	datadir := tmpdir(t)
	defer os.RemoveAll(datadir)

	// Start a Geth instance with the requested flags set and immediately terminate
	if genesis != "" {
		json := filepath.Join(datadir, "genesis.json")
		if err := ioutil.WriteFile(json, []byte(genesis), 0600); err != nil {
			t.Fatalf("test %d: failed to write genesis file: %v", test, err)
		}
		runGeth(t, "--datadir", datadir, "--networkid", "1337", "init", json).WaitExit()
	} else {
		// Force chain initialization
		args := []string{"--port", "0", "--networkid", "1337", "--maxpeers", "0", "--nodiscover", "--nat", "none", "--ipcdisable", "--datadir", datadir}
		runGeth(t, append(args, []string{"--exec", "2+2", "console"}...)...).WaitExit()
	}
	// Retrieve the DAO config flag from the database
	path := filepath.Join(datadir, "geth", "chaindata")
	db, err := rawdb.NewLevelDBDatabase(path, 0, 0, "", false)
	if err != nil {
		t.Fatalf("test %d: failed to open test database: %v", test, err)
	}
	defer db.Close()

	genesisHash := common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")
	if genesis != "" {
		genesisHash = daoGenesisHash
	}
	config := rawdb.ReadChainConfig(db, genesisHash)
	if config == nil {
		return // we want to return here, the other checks can't make it past this point (nil panic).
	}
	// Validate the DAO hard-fork block number against the expected value
	if config.DAOForkBlock == nil {
		if expectBlock != nil {
			t.Errorf("test %d: dao hard-fork block mismatch: have nil, want %v", test, expectBlock)
		}
	} else if expectBlock == nil {
		t.Errorf("test %d: dao hard-fork block mismatch: have %v, want nil", test, config.DAOForkBlock)
	} else if config.DAOForkBlock.Cmp(expectBlock) != 0 {
		t.Errorf("test %d: dao hard-fork block mismatch: have %v, want %v", test, config.DAOForkBlock, expectBlock)
	}
	if config.DAOForkSupport != expectVote {
		t.Errorf("test %d: dao hard-fork support mismatch: have %v, want %v", test, config.DAOForkSupport, expectVote)
	}
}
