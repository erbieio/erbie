package web2msg

import (
	"encoding/json"
	"errors"
)

const MASTODON_APP = "mastodon"

func NewAdapter(app string, body []byte) (Web2MsgInterface, error) {
	if app == MASTODON_APP {
		msg := &MastodonMsg{}
		err := json.Unmarshal(body, msg)
		if err != nil {
			return nil, err
		}
		return msg, nil
	}
	return nil, errors.New("unknown msg")
}
