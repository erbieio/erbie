package ibftengine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	ibfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/ibft/types"
	"github.com/ethereum/go-ethereum/consensus/istanbul/validator"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"golang.org/x/crypto/sha3"
)

var (
	nilUncleHash  = types.CalcUncleHash(nil)                 // Always Keccak256(RLP([])) as uncles are meaningless outside of PoW.
	nonceAuthVote = hexutil.MustDecode("0xffffffffffffffff") // Magic nonce number to vote on adding a new validator
	nonceDropVote = hexutil.MustDecode("0x0000000000000000") // Magic nonce number to vote on removing a validator.
)

// staleThreshold is the maximum depth of the acceptable stale block.
const staleThreshold = 7

type SignerFn func(data []byte) ([]byte, error)

type Option func(*types.IstanbulExtra)

func withValidators(vals []common.Address) Option {
	return func(ie *types.IstanbulExtra) {
		log.Info("withValidators", "vals", vals)
		ie.Validators = vals
	}
}

func WithSeal(seal []byte) Option {
	return func(ie *types.IstanbulExtra) {
		ie.Seal = seal
	}
}

func WithCommitSeal(commitSeal [][]byte) Option {
	return func(ie *types.IstanbulExtra) {
		ie.CommittedSeal = commitSeal
	}
}

func withExchangerAddr(exchangerAddr []common.Address) Option {
	return func(ie *types.IstanbulExtra) {
		ie.ExchangerAddr = exchangerAddr
	}
}

func withValidatorAddr(validatorAddr []common.Address) Option {
	return func(ie *types.IstanbulExtra) {
		ie.ValidatorAddr = validatorAddr
	}
}

func WithRewardSeal(rewardSeal [][]byte) Option {
	return func(ie *types.IstanbulExtra) {
		ie.RewardSeal = rewardSeal
	}
}

func WithEmptyBlockMessages(emptyBlockMessages [][]byte) Option {
	return func(ie *types.IstanbulExtra) {
		ie.EmptyBlockMessages = emptyBlockMessages
	}
}

func WithEvilAction(ea *types.EvilAction) Option {
	return func(ie *types.IstanbulExtra) {
		ie.EvilAction = ea
	}
}

type Engine struct {
	cfg *istanbul.Config

	signer  common.Address // Ethereum address of the signing key
	sign    SignerFn       // Signer function to authorize hashes with
	backend istanbul.Backend
}

func NewEngine(cfg *istanbul.Config, signer common.Address, sign SignerFn, backend istanbul.Backend) *Engine {
	return &Engine{
		cfg:     cfg,
		signer:  signer,
		sign:    sign,
		backend: backend,
	}
}

func (e *Engine) Author(header *types.Header) (common.Address, error) {
	// Retrieve the signature from the header extra-data
	extra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return common.Address{}, err
	}

	if header.Coinbase == common.HexToAddress("0x0000000000000000000000000000000000000000") && header.Number.Cmp(common.Big0) > 0 {
		return common.HexToAddress("0x0000000000000000000000000000000000000000"), nil
	} else {
		addr, err := istanbul.GetSignatureAddress(sigHash(header).Bytes(), extra.Seal)
		if err != nil {
			return addr, err
		}
		return addr, nil
	}

}

func (e *Engine) CommitHeader(header *types.Header, seals [][]byte, round *big.Int) error {
	// Append seals into extra-data
	return writeCommittedSeals(header, seals)
}

func (e *Engine) VerifyBlockProposal(chain consensus.ChainHeaderReader, block *types.Block, validators istanbul.ValidatorSet) (time.Duration, error) {
	// check block body
	txnHash := types.DeriveSha(block.Transactions(), new(trie.Trie))
	if txnHash != block.Header().TxHash {
		return 0, istanbulcommon.ErrMismatchTxhashes
	}

	// uncleHash := types.CalcUncleHash(block.Uncles())
	// if uncleHash != nilUncleHash {
	// 	return 0, istanbulcommon.ErrInvalidUncleHash
	// }

	if block.Coinbase() == common.HexToAddress("0x0000000000000000000000000000000000000000") && block.Number().Cmp(common.Big0) > 0 {
		return 0, istanbulcommon.ErrEmptyBlock
	} else {
		// verify the header of proposed block
		err := e.VerifyHeader(chain, block.Header(), nil, validators)
		if err == nil || err == istanbulcommon.ErrEmptyCommittedSeals {
			// ignore errEmptyCommittedSeals error because we don't have the committed seals yet
			return 0, nil
		} else if err == consensus.ErrFutureBlock {
			return time.Until(time.Unix(int64(block.Header().Time), 0)), consensus.ErrFutureBlock
		}
		return 0, err
	}

}

func (e *Engine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header, validators istanbul.ValidatorSet) error {
	return e.verifyHeader(chain, header, parents, validators)
}

// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (e *Engine) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header, validators istanbul.ValidatorSet) error {
	if header.Number == nil {
		return istanbulcommon.ErrUnknownBlock
	}

	// Don't waste time checking blocks from the future (adjusting for allowed threshold)
	adjustedTimeNow := time.Now().Add(time.Duration(e.cfg.AllowedFutureBlockTime) * time.Second).Unix()
	// log.Info("verifyHeader:futureBlock",
	// 	"no", header.Number,
	// 	"AllowedFutureBlockTime", e.cfg.AllowedFutureBlockTime,
	// 	"header.Time", header.Time,
	// 	"adjustedTimeNow", adjustedTimeNow,
	// 	"now", time.Now().Unix())
	if header.Time > uint64(adjustedTimeNow) {
		return consensus.ErrFutureBlock
	}

	if _, err := types.ExtractIstanbulExtra(header); err != nil {
		return istanbulcommon.ErrInvalidExtraDataFormat
	}

	if header.Nonce != (istanbulcommon.EmptyBlockNonce) && !bytes.Equal(header.Nonce[:], nonceAuthVote) && !bytes.Equal(header.Nonce[:], nonceDropVote) {
		return istanbulcommon.ErrInvalidNonce
	}

	// Ensure that the mix digest is zero as we don't have fork protection currently
	if header.MixDigest != types.IstanbulDigest {
		return istanbulcommon.ErrInvalidMixDigest
	}

	// Ensure that the block doesn't contain any uncles which are meaningless in Istanbul
	// if header.UncleHash != nilUncleHash {
	// 	return istanbulcommon.ErrInvalidUncleHash
	// }

	// Ensure that the block's difficulty is meaningful (may not be correct at this point)
	if header.Coinbase == common.HexToAddress("0x0000000000000000000000000000000000000000") && header.Number.Cmp(common.Big0) > 0 {
		if header.Difficulty == nil || header.Difficulty.Cmp(big.NewInt(24)) != 0 {
			return istanbulcommon.ErrInvalidDifficulty
		}
	} else {
		if header.Difficulty == nil || header.Difficulty.Cmp(istanbulcommon.DefaultDifficulty) != 0 {
			return istanbulcommon.ErrInvalidDifficulty
		}
	}

	return e.verifyCascadingFields(chain, header, validators, parents)
}

// verifyCascadingFields verifies all the header fields that are not standalone,
// rather depend on a batch of previous headers. The caller may optionally pass
// in a batch of parents (ascending order) to avoid looking those up from the
// database. This is useful for concurrently verifying a batch of new headers.
func (e *Engine) verifyCascadingFields(chain consensus.ChainHeaderReader, header *types.Header, validators istanbul.ValidatorSet, parents []*types.Header) error {
	// The genesis block is the always valid dead-end
	number := header.Number.Uint64()
	if number == 0 {
		return nil
	}

	// Check parent
	var parent *types.Header
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}

	// Ensure that the block's parent has right number and hash
	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return consensus.ErrUnknownAncestor
	}

	// Ensure that the block's timestamp isn't too close to it's parent
	if parent.Time+e.cfg.BlockPeriod > header.Time {
		return istanbulcommon.ErrInvalidTimestamp
	}

	if header.Coinbase == common.HexToAddress("0x0000000000000000000000000000000000000000") && header.Number.Cmp(common.Big0) > 0 {
		//err := e.verifyEmptyVote(chain, header, parents, validators)
		//if err != nil {
		//	return fmt.Errorf("verify empty block %v", err)
		//}
		return nil
	}

	// Verify signer
	if err := e.verifySigner(chain, header, parents, validators); err != nil {
		return err
	}

	return e.verifyCommittedSeals(chain, header, parents, validators)
}

func (e *Engine) verifyEmptyVote(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header, validators istanbul.ValidatorSet) error {
	var allWeightBalance = big.NewInt(0)
	var voteBalance *big.Int
	var coe uint8

	log.Info("azh|check empty vote")

	//averageCoefficient := bc.GetAverageCoefficient(statedb)
	bc, ok := chain.(*core.BlockChain)
	if !ok {
		return errors.New("chain assert failed")
	}

	parent := chain.GetHeaderByHash(header.ParentHash)
	stateDb, err := bc.StateAt(parent.Root)
	if err != nil {
		log.Error("azh|stateDb", "err", err)
		return errors.New("new statdb failed")
	}

	log.Info("azh|stateDb", "height", bc.CurrentHeader().Number, "empty height", header.Number)
	validatorList := stateDb.GetValidators(types.ValidatorStorageAddress)
	if validatorList == nil {
		log.Error("azh|validatorList", "err", err)
		return errors.New("get validators error")
	}

	extra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return err
	}

	for _, validator := range validatorList.Validators {
		coe = stateDb.GetValidatorCoefficient(validator.Addr)
		voteBalance = new(big.Int).Mul(validator.Balance, big.NewInt(int64(coe)))
		allWeightBalance.Add(allWeightBalance, voteBalance)
	}
	allWeightBalance50 := new(big.Int).Mul(big.NewInt(50), allWeightBalance)
	allWeightBalance50 = new(big.Int).Div(allWeightBalance50, big.NewInt(100))

	var votevValidators []common.Address
	for _, emptyBlockMessage := range extra.EmptyBlockMessages[1:] {
		flag, height := CheckHeight(header, emptyBlockMessage)
		log.Info("empty block check", "block height", header.Number, "vote height", height)
		if !flag {
			return errors.New("the vote height doesn`t match the block height")
		}
		msg := &types.EmptyMsg{}
		sender, err := msg.RecoverAddress(emptyBlockMessage)
		if err != nil {
			return err
		}
		votevValidators = append(votevValidators, sender)
	}

	var blockWeightBalance = big.NewInt(0)
	for _, v := range votevValidators {
		voteBalance = new(big.Int).Mul(validatorList.StakeBalance(v), big.NewInt(types.DEFAULT_VALIDATOR_COEFFICIENT))
		blockWeightBalance.Add(blockWeightBalance, voteBalance)
	}
	if blockWeightBalance.Cmp(allWeightBalance50) > 0 {
		return nil
	} else {
		log.Error("BlockChain.VerifyEmptyBlock(), verify validators of empty block error ",
			"blockWeightBalance", blockWeightBalance, "allWeightBalance50", allWeightBalance50)
		return errors.New("verify validators of empty block error")
	}
}

func CheckHeight(header *types.Header, emptyMsg []byte) (bool, *big.Int) {
	msg := new(types.EmptyMsg)
	if err := msg.FromPayload(emptyMsg); err != nil {
		return false, nil
	}

	var signature *types.SignatureData
	err := msg.Decode(&signature)
	if err != nil {
		return false, nil
	}

	if header.Number.Cmp(signature.Height) == 0 {
		return true, signature.Height
	}
	return false, signature.Height
}

func (e *Engine) verifySigner(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header, validators istanbul.ValidatorSet) error {
	// Verifying the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return istanbulcommon.ErrUnknownBlock
	}

	// Resolve the authorization key and check against signers
	signer, err := e.Author(header)

	if err != nil {
		return err
	}

	if _, v := validators.GetByAddress(signer); v == nil {
		log.Error("cavar|verifySigner-signer", "no", header.Number.Text(10), "header", header.Hash().Hex(), "sign", signer.Hex())
		for _, addr := range validators.List() {
			log.Error("cavar|verifySigner-val", "no", header.Number.Text(10), "header", header.Hash().Hex(), "val-addr", addr.Address().Hex())
		}
		return istanbulcommon.ErrUnauthorized
	}

	return nil
}

// verifyCommittedSeals checks whether every committed seal is signed by one of the parent's validators
func (e *Engine) verifyCommittedSeals(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header, validators istanbul.ValidatorSet) error {
	number := header.Number.Uint64()

	if number == 0 {
		// We don't need to verify committed seals in the genesis block
		return nil
	}

	extra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return err
	}
	committedSeal := extra.CommittedSeal

	if header.Coinbase == common.HexToAddress("0x0000000000000000000000000000000000000000") && header.Number.Cmp(common.Big0) > 0 {
		return nil
	} else {
		// The length of Committed seals should be larger than 0
		if len(committedSeal) == 0 {
			return istanbulcommon.ErrEmptyCommittedSeals
		}
	}

	validatorsCpy := validators.Copy(nil)

	// Check whether the committed seals are generated by validators
	validSeal := 0
	committers, err := e.Signers(header)
	if err != nil {
		return err
	}

	for _, addr := range committers {
		if validatorsCpy.RemoveValidator(addr) {
			validSeal++
			continue
		}
		log.Error("caver|verifyCommittedSeals|committers", "no", header.Number, "header", header.Hash(), "addr", addr)
		for _, addr := range validators.List() {
			log.Error("caver|verifyCommittedSeals|validatorset", "no", header.Number.Text(10), "addr", addr)
		}
		for _, addr := range committers {
			log.Info("caver|verifyCommittedSeals|committedseals", "no", header.Number, "addr", addr)
		}
		return istanbulcommon.ErrInvalidCommittedSeals
	}

	// The length of validSeal should be larger than number of faulty node + 1
	if validSeal <= validators.F() {
		log.Error("caver|verifyCommittedSeals|validSeal", "no", header.Number.Text(10), "validSeal_len", validSeal, "validators.F()", validators.F())
		return istanbulcommon.ErrInvalidCommittedSeals
	}

	return nil
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of a given engine.
func (e *Engine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	// if len(block.Uncles()) > 0 {
	// 	return istanbulcommon.ErrInvalidUncleHash
	// }
	return nil
}

// VerifySeal checks whether the crypto seal on a header is valid according to
// the consensus rules of the given engine.
func (e *Engine) VerifySeal(chain consensus.ChainHeaderReader, header *types.Header, validators istanbul.ValidatorSet) error {

	// get parent header and ensure the signer is in parent's validator set
	number := header.Number.Uint64()
	if number == 0 {
		return istanbulcommon.ErrUnknownBlock
	}

	// ensure that the difficulty equals to istanbulcommon.DefaultDifficulty
	if header.Difficulty.Cmp(istanbulcommon.DefaultDifficulty) != 0 {
		return istanbulcommon.ErrInvalidDifficulty
	}

	return e.verifySigner(chain, header, nil, validators)
}

func (e *Engine) Prepare(chain consensus.ChainHeaderReader, header *types.Header, validators istanbul.ValidatorSet) error {
	if header.Coinbase == common.HexToAddress("0x0000000000000000000000000000000000000000") &&
		header.Number.Cmp(common.Big0) > 0 {
		return errors.New("not a normal block")
	}
	header.Nonce = istanbulcommon.EmptyBlockNonce
	header.MixDigest = types.IstanbulDigest

	// copy the parent extra data as the header extra data
	number := header.Number.Uint64()
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}

	// use the same difficulty for all blocks
	header.Difficulty = istanbulcommon.DefaultDifficulty
	var (
		validatorAddr []common.Address
		exchangerAddr []common.Address
		rewardSeals   [][]byte
		evilAction    = &types.EvilAction{}
	)
	if c, ok := chain.(*core.BlockChain); ok {
		if header.Number.Uint64() == 1 {
			// Block 1 does not issue any rewards
			validatorAddr = make([]common.Address, 0)
		} else {
			parent := c.GetBlockByHash(header.ParentHash)
			if parent == nil {
				log.Error("Prepare: invalid parent", "no", header.Number)
				return errors.New("Prepare: invalid parent")
			}
			// quorum Size
			random11Validators, err := c.Random11ValidatorWithOutProxy(parent.Header())
			if err != nil {
				log.Error("Prepare : invalid validators", err.Error())
				return errors.New("Prepare: invalid validators")
			}
			quorumSize := e.QuorumSize(random11Validators.Len())
			if quorumSize == 0 {
				log.Error("Prepare invalid quorum size", "no", header.Number, "size", quorumSize)
				return errors.New("invalid quorum size")
			}
			log.Info("Prepare quorum size", "no", header.Number, "size", quorumSize)
			// Get the header of the last normal block
			preHeader, err := getPreHash(chain, header)
			if !preHeader.EmptyBlock() {
				if err != nil {
					log.Error("Prepare get preHash err", "err", err, "no", header.Number, "hash", header.Hash().Hex())
					return err
				}
				log.Info("Prepare getPreHash ok", "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
				commiters, err := e.Signers(preHeader)
				if err != nil {
					log.Error("Prepare commit seal err", "err", err.Error(), "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
					return err
				}
				if len(commiters) < quorumSize {
					log.Error("Prepare commiters len less than 7", "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
					return errors.New("Prepare commiters len less than 7")
				}
				for _, v := range commiters {
					if len(validatorAddr) == quorumSize {
						break
					}
					// reward to onlineValidtors
					validatorAddr = append(validatorAddr, v)
				}
				for i, v := range commiters {
					log.Info("print committers", "len", len(commiters), "i", i, "addr", v.Hex(), "preHeader", preHeader.Number, "no", header.Number)
				}
				for _, v := range validatorAddr {
					log.Info("Prepare: onlineValidator", "addr", v.Hex(), "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
				}
				// copy commitSeals to rewardSeals
				rewardSeals, err = e.copyCommitSeals(preHeader)
				if err != nil {
					log.Error("copy commitSeals err", "err", err, "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
					return err
				}
			}
		}

		// reward to openExchangers
		statedb, err := c.StateAt(parent.Root)
		if err != nil {
			log.Error("Engine: Prepare", "err", err, "no", header.Number.Uint64())
			return err
		}
		stakeList := statedb.GetStakers(types.StakerStorageAddress)
		if stakeList == nil {
			log.Error("Engine: Prepare get stakers error", "no", parent.Number.Uint64())
			return errors.New("get stakers error")
		}
		var benifitedStakers []common.Address

		validatorList := statedb.GetValidators(types.ValidatorStorageAddress)
		if validatorList == nil {
			log.Error("Engine: Prepare get validators error", "no", parent.Number.Uint64())
			return errors.New("get validators error")
		}

		stakers := statedb.GetStakers(types.StakerStorageAddress)
		if stakers == nil {
			log.Error("Engine: Prepare get stakers error", "no", parent.Number.Uint64())
			return errors.New("get stakers error")
		}

		// Obtain random landing points according to the surrounding chain algorithm
		randomHash := core.GetRandomDropV2(validatorList, stakers, parent)
		if randomHash == (common.Hash{}) {
			log.Error("Engine: Prepare : invalid random hash", "no", c.CurrentHeader().Number.Uint64())
			return err
		}
		log.Info("Engine: Prepare : drop", "no", header.Number.Uint64(), "randomHash", randomHash.Hex(), "header.hash", header.Hash().Hex())

		benifitedStakers, err = stakeList.SelectRandom4Address(types.StakerRewardNum, randomHash.Bytes())
		if err != nil {
			log.Error("Engine: Prepare", "SelectRandom4Address err", err, "no", c.CurrentHeader().Number.Uint64())
			return err
		}

		exchangerAddr = append(exchangerAddr, benifitedStakers...)

		//new&update  at 20220523
		if validatorList != nil && len(validatorList.Validators) > 0 {
			//k:proxy,v:validator
			mp := make(map[string]*types.Validator, 0)
			for _, v := range validatorList.Validators {
				if v.Proxy.String() != "0x0000000000000000000000000000000000000000" {
					mp[v.Proxy.String()] = v
				}
			}

			//The current block issues a reward for the previous block,
			//but if the participant in the previous block consensus sends a validator to cancel the transaction
			//and package it in the previous block, the current block does not send him a reward
			//for index, a := range validatorAddr {
			//	if !validatorList.Exist(a) {
			//		validatorAddr = append(validatorAddr[:index], validatorAddr[index+1:]...)
			//	}
			//}

			//If the reward address is on a proxy account, it will be restored to a pledge account
			for index, a := range validatorAddr {
				if v, ok := mp[a.String()]; ok {
					validatorAddr[index] = v.Addr
				}
			}
		}

		// Record the evil behavior of 7 blocks ago
		if header.Number.Uint64() > staleThreshold {
			ea, err := c.ReadEvilAction(header.Number.Uint64() - staleThreshold)
			if err == nil && ea != nil && !ea.Handled {
				evilAction = ea
				evilAction.Handled = true
			}
		}
	}

	// add validators in snapshot to extraData's validators section
	//extra, err := prepareExtra(header, validator.SortedAddresses(validators.List()), exchangerAddr, validatorAddr, rewardSeals, nil)
	extra, err := prepareExtraAdvanced(
		header,
		WithEvilAction(evilAction),
		WithRewardSeal(rewardSeals),
		withExchangerAddr(exchangerAddr),
		withValidatorAddr(validatorAddr),
		withValidators(validator.SortedAddresses(validators.List())),
	)
	if err != nil {
		return err
	}
	header.Extra = extra

	// set header's timestamp
	now := uint64(time.Now().Unix())
	header.Time = parent.Time + e.cfg.BlockPeriod
	if header.Time < now {
		header.Time = now
	}

	return nil
}

func prepareExtraAdvanced(header *types.Header, options ...Option) ([]byte, error) {
	var buf bytes.Buffer

	// compensate the lack bytes if header.Extra is not enough IstanbulExtraVanity bytes.
	if len(header.Extra) < types.IstanbulExtraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, types.IstanbulExtraVanity-len(header.Extra))...)
	}
	buf.Write(header.Extra[:types.IstanbulExtraVanity])

	h := &types.IstanbulExtra{
		// default options
		Validators:         []common.Address{},
		Seal:               []byte{},
		CommittedSeal:      [][]byte{},
		ExchangerAddr:      []common.Address{},
		ValidatorAddr:      []common.Address{},
		RewardSeal:         [][]byte{},
		EmptyBlockMessages: [][]byte{},
	}

	for _, option := range options {
		option(h)
	}

	payload, err := rlp.EncodeToBytes(&h)
	if err != nil {
		return nil, err
	}

	return append(buf.Bytes(), payload...), nil
}

// copy commit seal to reward seals
func (e *Engine) copyCommitSeals(header *types.Header) ([][]byte, error) {
	// extract istanbul extra
	extra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return nil, err
	}
	rewardSeals := make([][]byte, len(extra.CommittedSeal))
	for i, v := range extra.CommittedSeal {
		rewardSeals[i] = make([]byte, types.IstanbulExtraSeal)
		copy(rewardSeals[i][:], v[:])
	}
	return rewardSeals, nil
}

// getPreHash Get the header of the last normal header
func getPreHash(chain consensus.ChainHeaderReader, header *types.Header) (*types.Header, error) {
	preHeader := chain.GetHeaderByHash(header.ParentHash)
	if preHeader == nil {
		return nil, errors.New("getPreHash : invalid preHeader")
	}
	if preHeader.Number.Uint64() == 1 {
		// may be empty block
		return preHeader, nil
	}
	if preHeader.Coinbase == (common.Address{}) {
		preHeader, err := getPreHash(chain, preHeader)
		if err != nil {
			return nil, err
		}
		return preHeader, nil
	}
	return preHeader, nil
}

func (e *Engine) PrepareEmpty(chain consensus.ChainHeaderReader, header *types.Header, validators istanbul.ValidatorSet, emptyBlockMessages [][]byte) error {

	if header.Coinbase != common.HexToAddress("0x0000000000000000000000000000000000000000") {
		return errors.New("not a empty block")
	}

	header.Nonce = istanbulcommon.EmptyBlockNonce
	header.MixDigest = types.IstanbulDigest

	// copy the parent extra data as the header extra data
	number := header.Number.Uint64()
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = big.NewInt(24)

	// add validators in snapshot to extraData's validators section
	extra, err := prepareExtra(header, validator.GetAllVotes(validators.List()), nil, nil, nil, emptyBlockMessages)
	if err != nil {
		return err
	}
	header.Extra = extra

	// set header's timestamp
	header.Time = uint64(time.Now().Unix())

	return nil
}

func prepareExtra(header *types.Header, vals, exchangerAddr, validatorAddr []common.Address, rewardSeals [][]byte, emptyBlockMessages [][]byte) ([]byte, error) {
	var buf bytes.Buffer

	// compensate the lack bytes if header.Extra is not enough IstanbulExtraVanity bytes.
	if len(header.Extra) < types.IstanbulExtraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, types.IstanbulExtraVanity-len(header.Extra))...)
	}
	buf.Write(header.Extra[:types.IstanbulExtraVanity])

	ist := &types.IstanbulExtra{
		Validators:         vals,
		Seal:               []byte{},
		CommittedSeal:      [][]byte{},
		ExchangerAddr:      exchangerAddr,
		ValidatorAddr:      validatorAddr,
		RewardSeal:         rewardSeals,
		EmptyBlockMessages: emptyBlockMessages,
	}

	payload, err := rlp.EncodeToBytes(&ist)
	if err != nil {
		return nil, err
	}

	return append(buf.Bytes(), payload...), nil
}

// Finalize runs any post-transaction state modifications (e.g. block rewards)
// and assembles the final block.
//
// Note, the block header and state database might be updated to reflect any
// consensus rules that happen at finalization (e.g. block rewards).
func (e *Engine) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header) {
	c, ok := chain.(*core.BlockChain)
	if !ok {
		return
	}

	parent := c.GetBlockByHash(header.ParentHash)
	if parent == nil {
		return
	}

	// empty block  reduce 0.1weight and normal block add 0.5weight
	random11Validators, err := c.Random11ValidatorWithOutProxy(parent.Header())
	if err != nil {
		return
	}

	istanbulExtra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return
	}

	parentState, err := c.StateAt(parent.Root())
	if err != nil {
		log.Error("Engine.Finalize()", "get parent state error", err)
		return
	}
	pValidators := parentState.GetValidators(types.ValidatorStorageAddress)
	if pValidators == nil {
		log.Error("Engine.Finalize() get parent validators error", "block number", parent.NumberU64())
		return
	}

	randomDrop, err := c.GetRandomDrop(parent.Header())
	if err != nil {
		log.Error("Engine.Finalize()", "get randomDrop error", err)
		return
	}

	if header.Coinbase == (common.Address{}) {
		// reduce 1 weight
		for _, v := range random11Validators.Validators {
			state.SubValidatorCoefficient(v.Address(), 20)
		}

		voteAddrs := make([]common.Address, 0)
		emptyMsg := new(types.EmptyMsg)

		for _, emptyMessage := range istanbulExtra.EmptyBlockMessages {
			if err := emptyMsg.FromPayload(emptyMessage); err != nil {
				log.Error("Certify Failed to decode message from payload", "err", err)
				continue
			}
			//var signature *types.SignatureData
			//err = emptyMsg.Decode(&signature)
			//if err !=nil {
			//	log.Info("recoverMsg", "err", err)
			//	continue
			//}
			sender, err := emptyMsg.RecoverAddress(emptyMessage)
			//log.Info("recoverAddress", "msg", signature.Vote, "height", signature.Height, "sender", sender)
			if err != nil {
				log.Info("recover emptyMessage", "err", err)
				continue
			}

			for _, val := range pValidators.Validators {
				if val.Addr == sender || val.Proxy == sender {
					voteAddrs = append(voteAddrs, val.Addr)
					break
				}
			}
		}

		for _, vote := range voteAddrs[1:] {
			log.Info("AddValidatorCoefficient", "addr", vote)
			state.AddValidatorCoefficient(vote, 70)
		}
	} else {
		// add 2 weight
		for _, v := range istanbulExtra.ValidatorAddr {
			state.AddValidatorCoefficient(v, 20)
		}
	}

	if header.Coinbase == (common.Address{}) {
		state.CreateNFTByOfficial16(istanbulExtra.ValidatorAddr, istanbulExtra.ExchangerAddr, header.Number, randomDrop.Bytes())
		state.DistributeRewardsToStakers(istanbulExtra.ValidatorAddr, header.Number)
	} else {
		// pick 7 validator from rewardSeals
		var validatorAddr []common.Address
		if header.Number.Uint64() == 1 {
			// Block 1 does not issue any rewards
			validatorAddr = make([]common.Address, 0)
		} else {
			// quorum Size
			quorumSize := e.QuorumSize(random11Validators.Len())
			if quorumSize == 0 {
				log.Error("Finalize invalid quorum size", "no", header.Number, "size", quorumSize)
				return
			}
			log.Info("Finalize quorum size", "no", header.Number, "size", quorumSize)
			// Get the header of the last normal block
			preHeader, err := getPreHash(chain, header)
			if !preHeader.EmptyBlock() {
				if err != nil {
					log.Error("Finalize get preHash err", "err", err, "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
					return
				}
				log.Info("Finalize getPreHash ok", "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
				// decode rewards
				// preHeader + currentRewadSeal
				rewarders, err := e.RecoverRewards(preHeader, istanbulExtra.RewardSeal)
				if err != nil {
					log.Error("Finalize rewarders err", "err", err.Error(), "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
					return
				}
				for _, v := range rewarders {
					log.Info("Finalize: onlineValidator", "addr", v.Hex(), "len", len(rewarders), "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
				}
				if len(rewarders) < quorumSize {
					log.Error("Finalize commiters len less than 7", "preHeader", preHeader.Number, "preHash", preHeader.Hash().Hex(), "no", header.Number, "hash", header.Hash().Hex())
					return
				}
				for _, v := range rewarders {
					if len(validatorAddr) == quorumSize {
						break
					}
					// reward to onlineValidtors
					validatorAddr = append(validatorAddr, v)
				}

				if pValidators != nil && len(pValidators.Validators) > 0 {
					//k:proxy,v:validator
					mp := make(map[string]*types.Validator, 0)
					for _, v := range pValidators.Validators {
						if v.Proxy.String() != "0x0000000000000000000000000000000000000000" {
							mp[v.Proxy.String()] = v
						}
					}
					//If the reward address is on a proxy account, it will be restored to a pledge account
					for index, a := range validatorAddr {
						if v, ok := mp[a.String()]; ok {
							validatorAddr[index] = v.Addr
						}
					}
				}
			}
		}

		e.punishEvilValidators(c, state, istanbulExtra, header)

		state.CreateNFTByOfficial16(validatorAddr, istanbulExtra.ExchangerAddr, header.Number, randomDrop.Bytes())
		state.DistributeRewardsToStakers(validatorAddr, header.Number)
	}

	// Recalculate the weight, which needs to be calculated after the list is determined
	validatorStateObject := state.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
	validatorList := validatorStateObject.GetValidators().DeepCopy()
	for _, account := range validatorList.Validators {
		coefficient := state.GetValidatorCoefficient(account.Addr)
		validatorList.CalculateAddressRangeV2(account.Addr, account.Balance, big.NewInt(int64(coefficient)))
	}
	validatorStateObject.SetValidators(validatorList)

	/// No block rewards in Istanbul, so the state remains as is and uncles are dropped
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	header.UncleHash = nilUncleHash

}

// FinalizeAndAssemble implements consensus.Engine, ensuring no uncles are set,
// nor block rewards given, and returns the final block.
func (e *Engine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// Prepare reward address
	istanbulExtra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return nil, err
	}

	c, ok := chain.(*core.BlockChain)
	if !ok {
		return nil, nil
	}

	parent := c.GetBlockByHash(header.ParentHash)
	if parent == nil {
		return nil, istanbul.ErrParent
	}

	// empty block  reduce 0.1weight and normal block add 0.5weight
	random11Validators, err := c.Random11ValidatorWithOutProxy(parent.Header())
	if err != nil {
		return nil, err
	}

	randomDrop, err := c.GetRandomDrop(parent.Header())
	if err != nil {
		log.Error("Engine.Finalize()", "get randomDrop error", err)
		return nil, err
	}

	if header.Coinbase == (common.Address{}) {
		for _, v := range random11Validators.Validators {
			state.SubValidatorCoefficient(v.Address(), 20)
		}

		for _, v := range istanbulExtra.Validators[1:] {
			state.AddValidatorCoefficient(v, 70)
		}
	} else {
		for _, v := range istanbulExtra.ValidatorAddr {
			state.AddValidatorCoefficient(v, 20)
		}

	}

	e.punishEvilValidators(c, state, istanbulExtra, header)

	state.CreateNFTByOfficial16(istanbulExtra.ValidatorAddr, istanbulExtra.ExchangerAddr, header.Number, randomDrop.Bytes())
	state.DistributeRewardsToStakers(istanbulExtra.ValidatorAddr, header.Number)
	// Recalculate the weight, which needs to be calculated after the list is determined
	validatorStateObject := state.GetOrNewStakerStateObject(types.ValidatorStorageAddress)
	validatorList := validatorStateObject.GetValidators().DeepCopy()
	for _, account := range validatorList.Validators {
		coefficient := state.GetValidatorCoefficient(account.Addr)
		validatorList.CalculateAddressRangeV2(account.Addr, account.Balance, big.NewInt(int64(coefficient)))
	}
	validatorStateObject.SetValidators(validatorList)

	/// No block rewards in Istanbul, so the state remains as is and uncles are dropped
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	header.UncleHash = nilUncleHash

	// Assemble and return the final block for sealing
	return types.NewBlock(header, txs, uncles, receipts, new(trie.Trie)), nil
}

// @dev Punish the verifier who signs more
func (e *Engine) punishEvilValidators(bc *core.BlockChain, state *state.StateDB, extra *types.IstanbulExtra, header *types.Header) {
	ea := extra.EvilAction
	if header.Number.Uint64() <= staleThreshold || ea == nil || len(ea.EvilHeaders) == 0 {
		return
	}

	parent := bc.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return
	}

	valset := state.GetValidators(types.ValidatorStorageAddress)
	if valset == nil {
		log.Error("punishEvilValidators, get validators error")
		return
	}

	log.Info("enter punishEvilValidators", "curNo", header.Number.Uint64())

	var evilValidators []common.Address

	evilValidators = e.pickEvilValidatorsV2(bc, ea)

	var noProxyValidators []common.Address
	for _, v := range evilValidators {
		evilAddr := valset.GetValidatorAddr(v)
		if evilAddr == (common.Address{}) {
			continue
		}
		noProxyValidators = append(noProxyValidators, evilAddr)
		log.Info("final punishEvilValidators", "addr", evilAddr, "curNo", header.Number.Uint64())
	}

	state.PunishEvilValidators(noProxyValidators, header.Number)
}

// @dev pickEvilValidators pick out  evil validators
func (e *Engine) pickEvilValidators(ea *types.EvilAction) []common.Address {
	var totalSigners []common.Address
	for _, header := range ea.EvilHeaders {
		log.Info("pickEvilValidators", "evil-no", header.Number.Uint64(), "evil-hash", header.Hash().Hex())
		signers, err := e.Signers(header)
		if err != nil {
			break
		}
		totalSigners = append(totalSigners, signers...)
	}
	duplicateElements := duplicateRemoval(totalSigners)
	return duplicateElements
}

func (e *Engine) pickEvilValidatorsV2(bc *core.BlockChain, ea *types.EvilAction) []common.Address {
	var (
		totalSigners []common.Address // All signatures at the same height for both canonical and uncles blocks
		canonicalNo  = ea.EvilHeaders[0].Number.Uint64()
	)

	// get canonical block signers
	canonicalHeader := bc.GetHeaderByNumber(canonicalNo)
	if canonicalHeader == nil {
		log.Crit("we shouldn't be unable to find the block at this height", "height", canonicalNo)
		return totalSigners
	}

	if canonicalHeader.Coinbase != (common.Address{}) {
		canonicalSigners, err := e.Signers(canonicalHeader)
		if err != nil {
			log.Error("failed to recover block signers", "height", canonicalNo)
			return totalSigners
		}
		totalSigners = append(totalSigners, canonicalSigners...)
	}

	for _, header := range ea.EvilHeaders {
		log.Info("pickEvilValidators", "evil-no", header.Number.Uint64(), "evil-hash", header.Hash().Hex())
		signers, err := e.Signers(header)
		if err != nil {
			break
		}
		totalSigners = append(totalSigners, signers...)
	}

	duplicateElements := common.FindDup(totalSigners)
	return duplicateElements
}

// @dev Use map to return duplicate elements
// pick evil validators
func duplicateRemoval(target []common.Address) (duplicateElements []common.Address) {
	temp := make(map[common.Address]struct{})
	for _, v := range target {
		_, ok := temp[v]
		if !ok {
			temp[v] = struct{}{}
		} else {
			duplicateElements = append(duplicateElements, v)
		}
	}

	// remove duplication address from evil validators
	temp = make(map[common.Address]struct{})
	evilValidators := make([]common.Address, 0)
	for _, addr := range duplicateElements {
		if _, ok := temp[addr]; !ok {
			temp[addr] = struct{}{}
			evilValidators = append(evilValidators, addr)
		}
	}

	return evilValidators
}

// Seal generates a new block for the given input block with the local miner's
// seal place on top.
func (e *Engine) Seal(chain consensus.ChainHeaderReader, block *types.Block, validators istanbul.ValidatorSet) (*types.Block, error) {
	// update the block header timestamp and signature and propose the block to core engine
	header := block.Header()
	number := header.Number.Uint64()

	if header.Coinbase == common.HexToAddress("0x0000000000000000000000000000000000000000") && header.Number.Cmp(common.Big0) > 0 {
		parent := chain.GetHeader(header.ParentHash, number-1)
		if parent == nil {
			return block, consensus.ErrUnknownAncestor
		}

		return e.updateBlock(parent, block)
	} else {
		if _, v := validators.GetByAddress(e.signer); v == nil {
			return block, istanbulcommon.ErrUnauthorized
		}
		parent := chain.GetHeader(header.ParentHash, number-1)
		if parent == nil {
			return block, consensus.ErrUnknownAncestor
		}

		return e.updateBlock(parent, block)
	}

}

// update timestamp and signature of the block based on its number of transactions
func (e *Engine) updateBlock(parent *types.Header, block *types.Block) (*types.Block, error) {
	// sign the hash
	header := block.Header()
	seal, err := e.sign(sigHash(header).Bytes())
	if err != nil {
		return nil, err
	}

	err = writeSeal(header, seal)
	if err != nil {
		return nil, err
	}

	return block.WithSeal(header), nil
}

// writeSeal writes the extra-data field of the given header with the given seals.
// suggest to rename to writeSeal.
func writeSeal(h *types.Header, seal []byte) error {
	if len(seal)%types.IstanbulExtraSeal != 0 {
		return istanbulcommon.ErrInvalidSignature
	}

	istanbulExtra, err := types.ExtractIstanbulExtra(h)
	if err != nil {
		return err
	}

	istanbulExtra.Seal = seal
	payload, err := rlp.EncodeToBytes(&istanbulExtra)
	if err != nil {
		return err
	}

	h.Extra = append(h.Extra[:types.IstanbulExtraVanity], payload...)
	return nil
}

func (e *Engine) SealHash(header *types.Header) common.Hash {
	return sigHash(header)
}

func (e *Engine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return new(big.Int)
}

func (e *Engine) Validators(header *types.Header) ([]common.Address, error) {
	extra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return nil, err
	}

	return extra.Validators, nil
}

func (e *Engine) Signers(header *types.Header) ([]common.Address, error) {
	extra, err := types.ExtractIstanbulExtra(header)
	if err != nil {
		return []common.Address{}, err
	}
	committedSeal := extra.CommittedSeal
	proposalSeal := PrepareCommittedSeal(header.Hash())

	var addrs []common.Address
	// 1. Get committed seals from current header
	for _, seal := range committedSeal {
		// 2. Get the original address by seal and parent block hash
		addr, err := istanbulcommon.GetSignatureAddress(proposalSeal, seal)
		if err != nil {
			return nil, istanbulcommon.ErrInvalidSignature
		}
		addrs = append(addrs, addr)
	}

	return addrs, nil
}

func (e *Engine) RecoverRewards(header *types.Header, rewardSeal [][]byte) ([]common.Address, error) {
	// extra, err := types.ExtractIstanbulExtra(header)
	// if err != nil {
	// 	return []common.Address{}, err
	// }
	// rewardSeal := extra.RewardSeal
	proposalSeal := PrepareCommittedSeal(header.Hash())

	var addrs []common.Address
	// 1. Get committed seals from current header
	for _, seal := range rewardSeal {
		// 2. Get the original address by seal and parent block hash
		addr, err := istanbulcommon.GetSignatureAddress(proposalSeal, seal)
		if err != nil {
			return nil, istanbulcommon.ErrInvalidSignature
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func (e *Engine) Address() common.Address {
	return e.signer
}

func (e *Engine) WriteVote(header *types.Header, candidate common.Address, authorize bool) error {
	header.Coinbase = candidate
	if authorize {
		copy(header.Nonce[:], nonceAuthVote)
	} else {
		copy(header.Nonce[:], nonceDropVote)
	}

	return nil
}

func (e *Engine) ReadVote(header *types.Header) (candidate common.Address, authorize bool, err error) {
	switch {
	case bytes.Equal(header.Nonce[:], nonceAuthVote):
		authorize = true
	case bytes.Equal(header.Nonce[:], nonceDropVote):
		authorize = false
	default:
		return common.Address{}, false, istanbulcommon.ErrInvalidVote
	}

	return header.Coinbase, authorize, nil
}

// FIXME: Need to update this for Istanbul
// sigHash returns the hash which is used as input for the Istanbul
// signing. It is the hash of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.
func sigHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()
	rlp.Encode(hasher, types.IstanbulFilteredHeader(header, false))
	hasher.Sum(hash[:0])
	return hash
}

func writeCommittedSeals(h *types.Header, committedSeals [][]byte) error {
	if len(committedSeals) == 0 {
		return istanbulcommon.ErrInvalidCommittedSeals
	}

	for _, seal := range committedSeals {
		if len(seal) != types.IstanbulExtraSeal {
			return istanbulcommon.ErrInvalidCommittedSeals
		}
	}

	istanbulExtra, err := types.ExtractIstanbulExtra(h)
	if err != nil {
		return err
	}

	istanbulExtra.CommittedSeal = make([][]byte, len(committedSeals))
	copy(istanbulExtra.CommittedSeal, committedSeals)

	payload, err := rlp.EncodeToBytes(&istanbulExtra)
	if err != nil {
		return err
	}

	h.Extra = append(h.Extra[:types.IstanbulExtraVanity], payload...)
	return nil
}

// PrepareCommittedSeal returns a committed seal for the given hash
func PrepareCommittedSeal(hash common.Hash) []byte {
	var buf bytes.Buffer
	buf.Write(hash.Bytes())
	buf.Write([]byte{byte(ibfttypes.MsgCommit)})
	return buf.Bytes()
}

// helper func--------------------------------//

func BytesToInt(bys []byte) int {
	bytebuff := bytes.NewBuffer(bys)
	var data int32
	binary.Read(bytebuff, binary.BigEndian, &data)
	return int(data)
}

func (e *Engine) QuorumSize(valSize int) int {
	return 2*(int(math.Ceil(float64(valSize)/3))-1) + 1
}
