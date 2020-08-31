package v2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/godaner/geronimo/cipher"
	rule "github.com/godaner/geronimo/protocol"
)

var ErrDecrypt = errors.New("decrypt err")

// Header
type Header struct {
	HSeqN    uint16
	HAckN    uint16
	HFlag    uint16
	HWinSize uint16
	HAttrNum byte
}

func (h *Header) Flag() uint16 {
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
	C        cipher.Cipher
}

func (m *Message) SeqN() uint16 {
	return m.Header.SeqN()
}

func (m *Message) AckN() uint16 {
	return m.Header.AckN()
}

func (m *Message) Flag() uint16 {
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
func (m *Message) Marshall()  (bs []byte,err error) {
	buf := new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, m.Header.HSeqN)
	if err != nil {
		return nil,err
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HAckN)
	if err != nil {
		return nil,err
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HFlag)
	if err != nil {
		return nil,err
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HWinSize)
	if err != nil {
		return nil,err
	}
	err = binary.Write(buf, binary.BigEndian, m.Header.HAttrNum)
	if err != nil {
		return nil,err
	}
	for _, v := range m.Attr {
		err = binary.Write(buf, binary.BigEndian, v.AT)
		if err != nil {
			return nil,err
		}
		//be careful
		err = binary.Write(buf, binary.BigEndian, v.AL+3)
		if err != nil {
			return nil,err
		}
		err = binary.Write(buf, binary.BigEndian, v.AV)
		if err != nil {
			return nil,err
		}
	}
	return m.C.Encrypt(buf.Bytes()),nil
}

func (m *Message) UnMarshall(message []byte) (err error) {
	bs := m.C.Decrypt(message)
	if len(bs) <= 0 {
		return ErrDecrypt
	}
	buf := bytes.NewBuffer(bs)
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HSeqN); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HAckN); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HFlag); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HWinSize); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &m.Header.HAttrNum); err != nil {
		return err
	}

	m.Attr = make([]Attr, m.Header.AttrNum())
	m.AttrMaps = make(map[byte][]byte)
	for i := byte(0); i < m.Header.AttrNum(); i++ {
		attr := &m.Attr[i]
		err := binary.Read(buf, binary.BigEndian, &attr.AT)
		if err != nil {
			return err
		}
		err = binary.Read(buf, binary.BigEndian, &attr.AL)
		if err != nil {
			return err
		}
		attr.AL -= 3 //be careful
		attr.AV = make([]byte, attr.AL)
		if err := binary.Read(buf, binary.BigEndian, &attr.AV); err != nil {
			return err
		}
		m.AttrMaps[attr.AT] = attr.AV

	}
	return nil
}

func (m *Message) SYN1(seqN uint16) (mm rule.Message) {
	m.newMessage(rule.FlagSYN1, seqN, 0, 0)
	return m
}

func (m *Message) SYN2(seqN, ackN uint16) (mm rule.Message) {
	m.newMessage(rule.FlagSYN2, seqN, ackN, 0)
	return m
}

func (m *Message) FIN1(seqN uint16) (mm rule.Message) {
	m.newMessage(rule.FlagFIN1, seqN, 0, 0)
	return m
}

func (m *Message) FIN2(seqN, ackN uint16) (mm rule.Message) {
	m.newMessage(rule.FlagFIN2, seqN, ackN, 0)
	return m
}

func (m *Message) ACK(seqN, ackN, winSize uint16) (mm rule.Message) {
	m.newMessage(rule.FlagACK, seqN, ackN, winSize)
	return m
}
func (m *Message) PAYLOAD(seqN uint16, payload []byte) (mm rule.Message) {
	m.newMessage(rule.FlagPAYLOAD, seqN, 0, 0)
	m.Attr = []Attr{
		{
			AT: rule.AttrPAYLOAD, AL: uint16(len(payload)), AV: payload,
		},
	}
	m.Header.HAttrNum = byte(len(m.Attr))
	return m
}
func (m *Message) KeepAlive() (mm rule.Message) {
	m.newMessage(rule.FlagKeepAlive, 0, 0, 0)
	return m
}

func (m *Message) newMessage(flag, seqN, ackN uint16, winSize uint16) {
	m.Header = Header{
		HFlag:    flag,
		HSeqN:    seqN,
		HAckN:    ackN,
		HAttrNum: 0,
		HWinSize: winSize,
	}
}
