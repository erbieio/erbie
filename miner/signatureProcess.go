package miner

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
)

func (c *Certify) AssembleAndBroadcastMessage(height *big.Int) {
	//log.Info("AssembleAndBroadcastMessage", "validators len", len(c.stakers.Validators), "sender", c.addr, "vote index", c.voteIndex, "round", c.round)
	vote := c.stakers.Validators[c.voteIndex]
	var voteAddress common.Address
	if vote.Proxy == (common.Address{}) {
		voteAddress = vote.Addr
	} else {
		voteAddress = vote.Proxy
	}
	c.voteIndex++
	if c.voteIndex == len(c.stakers.Validators) {
		c.voteIndex = 0
		c.round++
	}

	log.Info("azh|start to vote", "validators", len(c.stakers.Validators), "vote", vote, "height:", height)
	err, payload := c.assembleMessage(height, voteAddress)
	if err != nil {
		return
	}

	if voteAddress == c.self {
		currentBlock := c.miner.GetWorker().eth.BlockChain().CurrentBlock()
		c.miner.GetWorker().mux.Post(core.NewMinedBlockEvent{Block: currentBlock})

		emptyMsg := types.EmptyMessageEvent{
			Sender:  c.self,
			Height:  height,
			Payload: payload,
		}
		go c.eventMux.Post(emptyMsg)
	} else {
		if miner, ok := c.miner.(*Miner); ok {
			miner.broadcaster.BroadcastEmptyBlockMsg(payload)
		}
	}
	//log.Info("AssembleAndBroadcastMessage end")
}

//func (c *Certify) SendSignToOtherPeer(vote common.Address, height *big.Int) {
//	log.Info("start SendSignToOtherPeer", "Address", vote.Hex(), "Height:", height)
//	ques := &types.SignatureData{
//		Vote:   vote,
//		Height: height,
//		//Timestamp: uint64(time.Now().Unix()),
//	}
//	encQues, err := Encode(ques)
//	if err != nil {
//		log.Error("Failed to encode", "subject", err)
//		return
//	}
//	c.broadcast(&types.EmptyMsg{
//		Code: SendSignMsg,
//		Msg:  encQues,
//	})
//}

//func (c *Certify) GetSignedMessage(height *big.Int) ([]byte, error) {
//	ques := &types.SignatureData{
//		Vote:   c.self,
//		Height: height,
//		//Timestamp: uint64(time.Now().Unix()),
//	}
//	encQues, err := Encode(ques)
//	if err != nil {
//		log.Error("GetSignedMessage Failed to encode", "subject", err)
//		return nil, err
//	}
//
//	msg := &types.EmptyMsg{
//		Code: SendSignMsg,
//		Msg:  encQues,
//	}
//
//	payload, err := c.signMessage(msg)
//	if err != nil {
//		log.Error("GetSignedMessage signMessage err", err)
//		return nil, err
//	}
//
//	return payload, nil
//}

func (c *Certify) GatherOtherPeerSignature(validator common.Address, height *big.Int, encQues []byte) error {
	var weightBalance *big.Int
	log.Info("GatherOtherPeerSignature", "c.proofStatePool", c.proofStatePool)
	if _, ok := c.proofStatePool.proofs[height.Uint64()]; !ok {
		_, proposerMessage := c.assembleMessage(height, c.self)
		ps := newProofState(c.self, proposerMessage, height)
		ps.receiveValidatorsSum = big.NewInt(0)
		//coe, err = c.miner.GetWorker().getValidatorCoefficient(validator)
		//if err != nil {
		//	return err
		//}
		//weightBalance = new(big.Int).Mul(validatorBalance, big.NewInt(int64(coe)))
		validatorBalance := c.stakers.StakeBalance(validator)
		weightBalance = new(big.Int).Mul(validatorBalance, big.NewInt(types.DEFAULT_VALIDATOR_COEFFICIENT))
		//weightBalance.Div(weightBalance, big.NewInt(10))
		ps.receiveValidatorsSum = new(big.Int).Add(ps.receiveValidatorsSum, weightBalance)
		//log.Info("Certify.GatherOtherPeerSignature", "validator", validator.Hex(), "balance", validatorBalance, "average coe", averageCoefficient, "weightBalance", weightBalance, "receiveValidatorsSum", ps.receiveValidatorsSum, "height", height.Uint64())
		//ps.onlineValidator.Add(validator)
		//ps.height = new(big.Int).Set(height)
		ps.onlineValidator = append(ps.onlineValidator, validator)
		ps.emptyBlockMessages = append(ps.emptyBlockMessages, encQues)

		c.proofStatePool.proofs[height.Uint64()] = ps
		//log.Info("GatherOtherPeerSignature", "height", height)
		c.signatureResultCh <- height
		//log.Info("GatherOtherPeerSignature end", "height", height)
		//log.Info("Certify.GatherOtherPeerSignature <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<< 1")
		return nil
	}

	curProofs := c.proofStatePool.proofs[height.Uint64()]
	if curProofs.onlineValidator.Has(validator) {
		return errors.New("GatherOtherPeerSignature: validator exist")
	}
	c.proofStatePool.proofs[height.Uint64()].onlineValidator = append(c.proofStatePool.proofs[height.Uint64()].onlineValidator, validator)
	c.proofStatePool.proofs[height.Uint64()].emptyBlockMessages = append(c.proofStatePool.proofs[height.Uint64()].emptyBlockMessages, encQues)
	//coe, err = c.miner.GetWorker().getValidatorCoefficient(validator)
	//if err != nil {
	//	return err
	//}
	//weightBalance = new(big.Int).Mul(validatorBalance, big.NewInt(int64(coe)))
	validatorBalance := c.stakers.StakeBalance(validator)
	weightBalance = new(big.Int).Mul(validatorBalance, big.NewInt(types.DEFAULT_VALIDATOR_COEFFICIENT))
	//weightBalance.Div(weightBalance, big.NewInt(10))
	c.proofStatePool.proofs[height.Uint64()].receiveValidatorsSum = new(big.Int).Add(c.proofStatePool.proofs[height.Uint64()].receiveValidatorsSum, weightBalance)
	//log.Info("Certify.GatherOtherPeerSignature", "validator", validator.Hex(), "balance", validatorBalance, "average coe", averageCoefficient, "weightBalance", weightBalance, "receiveValidatorsSum", c.proofStatePool.proofs[height.Uint64()].receiveValidatorsSum, "height", height.Uint64())
	//log.Info("Certify.GatherOtherPeerSignature", "receiveValidatorsSum", c.proofStatePool.proofs[height.Uint64()].receiveValidatorsSum, "heigh", height)
	c.signatureResultCh <- height
	//log.Info("Certify.GatherOtherPeerSignature <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<< 2")
	return nil
}
