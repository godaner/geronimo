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
	MaxSeqN = DefWinSize * 2
	MinSeqN = 0
)
const (
	MaxAckN = MaxSeqN
	MinAckN = MinSeqN
)
const (
	MaxWinSize = MaxSeqN
	//MinWinSize = 2
	//DefWinSize = 1024*1 // main !!!!!!!!
	DefWinSize = 512 // main !!!!!!!!
	//DefWinSize = 8 // for test
	//DefWinSize = 4 // for test
	//DefWinSize = 1 // for test
)
const (
	MSS = 1472-13
	//MSS = 1 // for test
)
const (
	//U1500 = uint32(1500)
)

type Header interface {
	SeqN() uint16
	AckN() uint16
	Flag() byte // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 PAYLOAD , 10000000 SCANWIN
	WinSize() uint16
	AttrNum() byte
}
