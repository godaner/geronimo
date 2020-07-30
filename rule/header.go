package rule

const (
	FlagSYN1    = 1 << iota
	FlagSYN2
	FlagSYN3
	FlagFIN1
	FlagFIN2
	FlagFIN3
	FlagFIN4
	FlagACK
	FlagPAYLOAD
)

const (
	_           = iota
	AttrPAYLOAD
)
const (
	MaxSeqN = uint32(2 * DefRecWinSize)
	MinSeqN = uint32(0)
)

const (
	DefCongWinSize = 1
	DefRecWinSize  = 16
)

const (
	DefSsthresh = DefRecWinSize / 2
)
const (
	MSS = 1472 - 18 // 1455
)

type Header interface {
	SeqN() uint32
	AckN() uint32
	Flag() uint16 // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 PAYLOAD , 10000000 SCANWIN
	WinSize() uint16
	AttrNum() byte
}
