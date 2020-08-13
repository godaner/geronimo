package v2

import (
	"bytes"
	"github.com/godaner/geronimo/rule"
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestMessage_ACK(t *testing.T) {
	Convey("ACK !", t, func(cc C) {
		seqN := uint16(251)
		ackN := uint16(333)
		winSize := uint16(4444)
		// m
		m := &Message{}
		m.ACK(seqN, ackN, winSize)
		bs := m.Marshall()
		So(bs, ShouldNotBeEmpty)

		// m1
		m1 := &Message{}
		err := m1.UnMarshall(bs)
		So(err, ShouldBeNil)
		So(seqN, ShouldEqual, m1.SeqN())
		So(rule.FlagACK, ShouldEqual, m1.Flag())
		So(ackN, ShouldEqual, m1.AckN())
		So(winSize, ShouldEqual, m1.WinSize())
		bs1 := m1.Marshall()
		So(len(bs), ShouldEqual, len(bs1))
		So(bytes.Compare(bs, bs1) == 0, ShouldBeTrue)

	})
}
func TestMessage_PAYLOAD(t *testing.T) {
	Convey("PAYLOAD !", t, func(cc C) {
		seqN := uint16(251)
		ackN := uint16(0)
		winSize := uint16(0)
		payload := []byte("this is payload")
		// m
		m := &Message{}
		m.PAYLOAD(seqN, payload)
		bs := m.Marshall()
		So(bs, ShouldNotBeEmpty)

		// m1
		m1 := &Message{}
		err := m1.UnMarshall(bs)
		So(err, ShouldBeNil)
		So(string(m1.AttributeByType(rule.AttrPAYLOAD)), ShouldEqual, string(payload))
		So(seqN, ShouldEqual, m1.SeqN())
		So(rule.FlagPAYLOAD, ShouldEqual, m1.Flag())
		So(ackN, ShouldEqual, m1.AckN())
		So(winSize, ShouldEqual, m1.WinSize())
		bs1 := m1.Marshall()
		So(len(bs), ShouldEqual, len(bs1))
		So(bytes.Compare(bs, bs1) == 0, ShouldBeTrue)

	})
}