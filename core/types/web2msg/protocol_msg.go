package web2msg

type ProtocolMsg struct {
	App          string        `json:"app"`
	ObjectUrl    string        `json:"object_url"`
	ReplyTo      string        `json:"reply_to"`
	OwnerAccount string        `json:"owner_account"`
	ToAccount    string        `json:"to_account"`
	Action       string        `json:"action"`
	Params       []interface{} `json:"params"`
}

type Web2Msgs []*ProtocolMsg

func (s Web2Msgs) Len() int { return len(s) }

func (m *ProtocolMsg) AppName() string {
	return m.App
}

// TODO
func (m *ProtocolMsg) CheckValid() error {
	return nil
}

// TODO
func (m *ProtocolMsg) From() error {
	return nil
}

// TODO
func (m *ProtocolMsg) To() error {
	return nil
}

// TODO
func (m *ProtocolMsg) Hash() []byte {
	return []byte{}
}
