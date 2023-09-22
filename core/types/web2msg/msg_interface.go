package web2msg

type Web2MsgInterface interface {
	AppName() string
	TransformProtocolMsg() *ProtocolMsg
	CheckValid() error
}
