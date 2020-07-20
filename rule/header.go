package rule

const (
	FlagFIN     = 1 << iota
	FlagSYN     = 1 << iota
	FlagRST     = 1 << iota
	FlagPSH     = 1 << iota
	FlagACK     = 1 << iota
	FlagURG     = 1 << iota
	FlagPAYLOAD = 1 << iota
	FlagSCANWIN = 1 << iota
)

const (
	_           = iota
	AttrPAYLOAD = iota // payload
)
const (
	MaxSeqN = DefRecWinSize * 2
	MinSeqN = 0
)
const (
	MaxAckN = MaxSeqN
	MinAckN = MinSeqN
)
const (
	DefCongWinSize = MSS * 1
	DefRecWinSize  = MSS * 10
)
const (
	MSS = 1472 - 13
)
const (
	DefSsthresh = MSS * 4
)

type Header interface {
	SeqN() uint16
	AckN() uint16
	Flag() byte // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 PAYLOAD , 10000000 SCANWIN
	WinSize() uint16
	AttrNum() byte
}
