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
	MaxSeqN = uint32(2 * DefRecWinSize)
	MinSeqN = uint32(0)
)

const (
	DefCongWinSize = 1
	DefRecWinSize  = 128
)

const (
	DefSsthresh = DefRecWinSize / 2
)
const (
	MSS = 1472 - 17 // 1455
)

type Header interface {
	SeqN() uint32
	AckN() uint32
	Flag() byte // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 PAYLOAD , 10000000 SCANWIN
	WinSize() uint16
	AttrNum() byte
}
