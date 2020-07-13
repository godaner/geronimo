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
	FINACK(seqN, ackN, winSize uint16)
	PAYLOAD(seqN uint16, payload []byte)
	SCANWIN(seqN uint16)

	// from head
	SeqN() uint16
	AckN() uint16
	Flag() byte
	WinSize() uint16
	AttrNum() byte
}
