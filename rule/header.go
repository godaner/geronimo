package rule

const (
	FlagSYN1 = 1 << iota
	FlagSYN2
	FlagFIN1
	FlagFIN2
	FlagACK
	FlagPAYLOAD
	FlagKeepAlive
)

const (
	_ = iota
	AttrPAYLOAD
)

type Header interface {
	SeqN() uint16
	AckN() uint16
	Flag() uint16 // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 PAYLOAD , 10000000 SCANWIN
	WinSize() uint16
	AttrNum() byte
}
