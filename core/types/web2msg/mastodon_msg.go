package web2msg

type MastodonMsg struct {
	App       string `json:"app"`
	ObjectUrl string `json:"object_url"`
	ReplyTo   string `json:"reply_to"`
}

func (m *MastodonMsg) AppName() string {
	return m.App
}

func (m *MastodonMsg) TransformProtocolMsg() *ProtocolMsg {
	return &ProtocolMsg{
		App:       m.App,
		ObjectUrl: m.ObjectUrl,
		ReplyTo:   m.ReplyTo,
	}
}

// TODO
func (m *MastodonMsg) CheckValid() error {
	return nil
}
