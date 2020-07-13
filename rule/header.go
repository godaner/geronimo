package rule

const (
	FlagFIN = 1
	FlagSYN = 2 << 0
	FlagRST = 2 << 1
	FlagPSH = 2 << 2
	FlagACK = 2 << 3
	FlagURG = 2 << 4
)

const (
	_       = iota
	AttrMSS = iota // mss = 1500(mtu) - len(header)
)

type Header interface {
	SeqN() uint16
	AckN() uint16
	Flag() byte // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 keep it , 1000000  keep it
	WinSize() uint16
	AttrNum() byte
}
