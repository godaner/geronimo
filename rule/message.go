package rule

type Message interface {
	UnMarshall(message []byte) (err error)
	Marshall() []byte
	Attribute(int) Attr
	AttributeByType(byte) []byte

	// op
	SYN1(seqN uint16)
	SYN2(seqN, ackN uint16)
	FIN1(seqN uint16)
	FIN2(seqN, ackN uint16)
	ACK(seqN, ackN, winSize uint16)
	PAYLOAD(seqN uint16, payload []byte)
	KeepAlive()

	// from head
	SeqN() uint16
	AckN() uint16
	Flag() uint16
	WinSize() uint16
	AttrNum() byte
}
