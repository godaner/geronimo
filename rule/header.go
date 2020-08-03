package rule

const (
	FlagSYN1    = 1 << iota // 1
	FlagSYN2                // 2
	FlagSYN3                // 4
	FlagFIN1                // 8
	FlagFIN2                // 16
	FlagFIN3                // 32
	FlagFIN4                // 64
	FlagACK                 // 128
	FlagPAYLOAD             // 256
)

const (
	_ = iota
	AttrPAYLOAD
)
const (
	MaxSeqN = uint32(2 * DefRecWinSize)
	MinSeqN = uint32(0)
)

const (
	DefCongWinSize = 1
	DefRecWinSize  = 4096
	//DefRecWinSize  = 512
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
