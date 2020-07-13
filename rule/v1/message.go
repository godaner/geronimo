package v1

import (
	"bytes"
	"encoding/binary"
	rule "github.com/godaner/geronimo/rule"
	"log"
)

// Header
type Header struct {
	HSeqN    uint16
	HAckN    uint16
	HFlag    uint8
	HWinSize uint16
	HAttrNum byte
}

func (h *Header) Flag() uint8 {
	return h.HFlag
}

func (h *Header) AttrNum() byte {
	return h.HAttrNum
}

func (h *Header) AckN() uint16 {
	return h.HAckN
}

func (h *Header) SeqN() uint16 {
	return h.HSeqN
}

func (h *Header) WinSize() uint16 {
	return h.HWinSize
}

// Attr
type Attr struct {
	AT byte
	AL uint16
	AV []byte
}

func (a *Attr) T() byte {
	return a.AT
}

func (a *Attr) L() uint16 {
	return a.AL
}

func (a *Attr) V() []byte {
	return a.AV
}

// Message
type Message struct {
	Header   Header
	Attr     []Attr
	AttrMaps map[byte][]byte
}

func (m *Message) SeqN() uint16 {
	return m.Header.SeqN()
}

func (m *Message) AckN() uint16 {
	return m.Header.AckN()
}

func (m *Message) Flag() byte {
	return m.Header.Flag()
}

func (m *Message) WinSize() uint16 {
	return m.Header.WinSize()
}

func (m *Message) AttrNum() byte {
	return m.Header.AttrNum()
}

func (m *Message) AttributeByType(t byte) []byte {
	return m.AttrMaps[t]
}

func (m *Message) Attribute(index int) rule.Attr {
	return &m.Attr[index]
}
func (m *Message) Marshall() []byte {
	buf := new(bytes.Buffer)
	var err error
	err = binary.Write(buf, binary.BigEndian, m.Header.HSeqN)
	if err != nil {
		log.Printf("Message#Bytes : binary.Write m.Header.HSeqN err , err is : %v !", err.Error())
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HAckN)
	if err != nil {
		log.Printf("Message#Bytes : binary.Write m.Header.HAckN err , err is : %v !", err.Error())
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HFlag)
	if err != nil {
		log.Printf("Message#Bytes : binary.Write m.Header.HFlag err , err is : %v !", err.Error())
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HWinSize)
	if err != nil {
		log.Printf("Message#Bytes : binary.Write m.Header.HWinSize err , err is : %v !", err.Error())
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HAttrNum)
	if err != nil {
		log.Printf("Message#Bytes : binary.Write m.Header.AttrNum err , err is : %v !", err.Error())
	}
	for _, v := range m.Attr {
		err = binary.Write(buf, binary.BigEndian, v.AT)
		if err != nil {
			log.Printf("Message#Bytes : binary.Write m.Header.AttrType err , err is : %v !", err.Error())
		}
		//be careful
		err = binary.Write(buf, binary.BigEndian, v.AL+3)
		if err != nil {
			log.Printf("Message#Bytes : binary.Write m.Header.AttrLen err , err is : %v !", err.Error())
		}
		err = binary.Write(buf, binary.BigEndian, v.AV)
		if err != nil {
			log.Printf("Message#Bytes : binary.Write m.Header.AttrStr err , err is : %v !", err.Error())
		}
	}
	return buf.Bytes()
}

func (m *Message) UnMarshall(message []byte) (err error) {
	buf := bytes.NewBuffer(message)
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HSeqN); err != nil {
		log.Printf("Message#UnMarshall : binary.Readm.Header.HSeqN err , err is : %v !", err.Error())
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HAckN); err != nil {
		log.Printf("Message#UnMarshall : binary.Readm.Header.HAckN err , err is : %v !", err.Error())
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HFlag); err != nil {
		log.Printf("Message#UnMarshall : binary.Readm.Header.HFlag err , err is : %v !", err.Error())
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HWinSize); err != nil {
		log.Printf("Message#UnMarshall : binary.Readm.Header.HWinSize err , err is : %v !", err.Error())
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HAttrNum); err != nil {
		log.Printf("Message#UnMarshall : binary.Readm.Header.HAttrNum err , err is : %v !", err.Error())
		return err
	}

	m.Attr = make([]Attr, m.Header.AttrNum())
	m.AttrMaps = make(map[byte][]byte)
	for i := byte(0); i < m.Header.AttrNum(); i++ {
		attr := &m.Attr[i]
		err := binary.Read(buf, binary.BigEndian, &attr.AT)
		if err != nil {
			log.Printf("Message#UnMarshall : binary.Read 0 err , err is : %v !", err.Error())
			return err
		}
		err = binary.Read(buf, binary.BigEndian, &attr.AL)
		if err != nil {
			log.Printf("Message#UnMarshall : binary.Read 1 err , err is : %v !", err.Error())
			return err
		}
		attr.AL -= 3 //be careful
		attr.AV = make([]byte, attr.AL)
		if err := binary.Read(buf, binary.BigEndian, &attr.AV); err != nil {
			log.Printf("Message#UnMarshall : binary.Read 2 err , err is : %v !", err.Error())
			return err
		}
		m.AttrMaps[attr.AT] = attr.AV

	}
	return nil
}

func (m *Message) SYN(seqN uint16) {
	m.newMessage(rule.FlagSYN, seqN, 0, 0)
}
func (m *Message) SYNACK(seqN, ackN, winSize uint16) {
	m.newMessage(rule.FlagSYN|rule.FlagACK, seqN, ackN, winSize)
}
func (m *Message) ACK(seqN, ackN, winSize uint16) {
	m.newMessage(rule.FlagACK, seqN, ackN, winSize)
}
func (m *Message) FINACK(seqN, ackN, winSize uint16) {
	m.newMessage(rule.FlagFIN|rule.FlagACK, seqN, ackN, winSize)
}
func (m *Message) PAYLOAD(seqN uint16, payload []byte) {
	m.newMessage(rule.FlagPAYLOAD, seqN, 0, 0)
	m.Attr = []Attr{
		{
			AT: rule.AttrPAYLOAD, AL: uint16(len(payload)), AV: payload,
		},
	}
	m.Header.HAttrNum = byte(len(m.Attr))
}
func (m *Message) SCANWIN(seqN uint16) {
	m.newMessage(rule.FlagSCANWIN, seqN, 0, 0)
}
func (m *Message) newMessage(flag uint8, seqN, ackN, winSize uint16) {
	m.Header = Header{
		HFlag:    flag,
		HSeqN:    seqN,
		HAckN:    ackN,
		HAttrNum: 0,
		HWinSize: winSize,
	}
}
