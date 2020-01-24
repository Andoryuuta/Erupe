package mhfpacket

import (
	"github.com/Andoryuuta/Erupe/network"
	"github.com/Andoryuuta/byteframe"
)

// MsgSysGetUserBinary represents the MSG_SYS_GET_USER_BINARY
type MsgSysGetUserBinary struct {
	AckHandle  uint32
	CharID     uint32
	BinaryType uint8
}

// Opcode returns the ID associated with this packet type.
func (m *MsgSysGetUserBinary) Opcode() network.PacketID {
	return network.MSG_SYS_GET_USER_BINARY
}

// Parse parses the packet from binary
func (m *MsgSysGetUserBinary) Parse(bf *byteframe.ByteFrame) error {
	m.AckHandle = bf.ReadUint32()
	m.CharID = bf.ReadUint32()
	m.BinaryType = bf.ReadUint8()
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgSysGetUserBinary) Build(bf *byteframe.ByteFrame) error {
	panic("Not implemented")
}
