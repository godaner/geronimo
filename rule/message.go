package rule

type Message interface {
	UnMarshall(message []byte) (err error)
	Marshall() []byte
	Attribute(int) Attr
	AttributeByType(byte) []byte

	// op
	SYN(seqN uint16)
	SYNACK(seqN, ackN, winSize uint16)
	ACK(seqN, ackN, winSize uint16)
	FIN(seqN uint16)
	FINACK(seqN, ackN uint16)

	// from head
	SeqN() uint16
	AckN() uint16
	Flag() byte // 00000001 FIN , 00000010 SYN , 00000100 RST , 00001000 PSH , 00010000 ACK , 00100000 URG , 01000000 keep it , 1000000  keep it
	WinSize() uint16
	AttrNum() byte
}
