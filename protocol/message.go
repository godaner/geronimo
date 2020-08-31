package protocol

type Message interface {
	UnMarshall(message []byte) (err error)
	Marshall() (bs []byte, err error)
	Attribute(int) Attr
	AttributeByType(byte) []byte

	// op
	SYN1(seqN uint16) (m Message)
	SYN2(seqN, ackN uint16) (m Message)
	FIN1(seqN uint16) (m Message)
	FIN2(seqN, ackN uint16) (m Message)
	ACK(seqN, ackN, winSize uint16) (m Message)
	PAYLOAD(seqN uint16, payload []byte) (m Message)
	KeepAlive() (m Message)

	// from head
	SeqN() uint16
	AckN() uint16
	Flag() uint16
	WinSize() uint16
	AttrNum() byte
}
