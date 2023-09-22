package core

import (
	"sync"

	"github.com/ethereum/go-ethereum/core/types/web2msg"
)

type MsgPool struct {
	mu      sync.RWMutex
	pending map[string]*web2msg.ProtocolMsg
}

func NewMsgPool() *MsgPool {
	return &MsgPool{
		pending: make(map[string]*web2msg.ProtocolMsg),
	}
}

func (p *MsgPool) Pending() []*web2msg.ProtocolMsg {
	p.pending["abc"] = &web2msg.ProtocolMsg{
		App:          "mastodon",
		ObjectUrl:    "baidu",
		ReplyTo:      "cc",
		OwnerAccount: "xwei",
		ToAccount:    "erbie",
		Action:       "MINT",
		Params:       []interface{}{"mint", "pic"},
	}
	res := make([]*web2msg.ProtocolMsg, 0)
	for _, m := range p.pending {
		res = append(res, m)
	}
	return res
}
