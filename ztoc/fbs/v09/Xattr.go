// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package v09

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type Xattr struct {
	_tab flatbuffers.Table
}

func GetRootAsXattr(buf []byte, offset flatbuffers.UOffsetT) *Xattr {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &Xattr{}
	x.Init(buf, n+offset)
	return x
}

func GetSizePrefixedRootAsXattr(buf []byte, offset flatbuffers.UOffsetT) *Xattr {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &Xattr{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func (rcv *Xattr) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *Xattr) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *Xattr) Key() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *Xattr) Value() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func XattrStart(builder *flatbuffers.Builder) {
	builder.StartObject(2)
}
func XattrAddKey(builder *flatbuffers.Builder, key flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(key), 0)
}
func XattrAddValue(builder *flatbuffers.Builder, value flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(value), 0)
}
func XattrEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
