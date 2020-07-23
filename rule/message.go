package rule

type Message interface {
	UnMarshall(message []byte) (err error)
	Marshall() []byte
	Attribute(int) Attr
	AttributeByType(byte) []byte

	// op
	SYN(seqN uint32)
	SYNACK(seqN, ackN uint32, winSize uint16)
	ACK(ackN uint32, winSize uint16)
	ACKN(ackN, seqN uint32, winSize uint16)
	FINACK(seqN, ackN uint32, winSize uint16)
	FIN(seqN uint32)
	PAYLOAD(seqN uint32, payload []byte)
	SCANWIN(seqN uint32)

	// from head
	SeqN() uint32
	AckN() uint32
	Flag() byte
	WinSize() uint16
	AttrNum() byte
}
