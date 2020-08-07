package rule

type Message interface {
	UnMarshall(message []byte) (err error)
	Marshall() []byte
	Attribute(int) Attr
	AttributeByType(byte) []byte

	// op
	SYN1(seqN uint32)
	SYN2(seqN, ackN uint32)
	SYN3(seqN, ackN uint32)
	FIN1(seqN uint32)
	FIN2(seqN, ackN uint32)
	FIN3(seqN, ackN uint32)
	FIN4(seqN, ackN uint32)
	ACK(ackN uint32, winSize uint16)
	PAYLOAD(seqN uint32, payload []byte)

	// from head
	SeqN() uint16
	AckN() uint16
	Flag() uint16
	WinSize() uint16
	AttrNum() byte
}
