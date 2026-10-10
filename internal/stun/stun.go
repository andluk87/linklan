// Package stun implements RFC 5389 IPv4 Binding requests on the SAME UDP socket
// as the tunnel. No dependency on an external public STUN operator is needed.
package stun

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
)

const Magic uint32 = 0x2112a442

func Request() ([]byte, error) {
	b := make([]byte, 20)
	binary.BigEndian.PutUint16(b, 1)
	binary.BigEndian.PutUint32(b[4:8], Magic)
	_, e := rand.Read(b[8:20])
	return b, e
}
func Is(b []byte) bool {
	return len(b) >= 20 && b[0]&0xc0 == 0 && binary.BigEndian.Uint32(b[4:8]) == Magic && int(binary.BigEndian.Uint16(b[2:4])) == len(b)-20
}
func Response(request []byte, addr *net.UDPAddr) ([]byte, error) {
	if !Is(request) || binary.BigEndian.Uint16(request[:2]) != 1 || len(request) > 512 || addr.IP.To4() == nil {
		return nil, errors.New("unsupported STUN request")
	}
	b := make([]byte, 32)
	binary.BigEndian.PutUint16(b, 0x101)
	binary.BigEndian.PutUint16(b[2:4], 12)
	copy(b[4:20], request[4:20])
	binary.BigEndian.PutUint16(b[20:22], 0x20)
	binary.BigEndian.PutUint16(b[22:24], 8)
	b[25] = 1
	binary.BigEndian.PutUint16(b[26:28], uint16(addr.Port)^uint16(Magic>>16))
	ip := addr.IP.To4()
	for i := 0; i < 4; i++ {
		b[28+i] = ip[i] ^ request[4+i]
	}
	return b, nil
}
func ParseResponse(b, transaction []byte) (*net.UDPAddr, error) {
	if !Is(b) || binary.BigEndian.Uint16(b[:2]) != 0x101 || len(transaction) != 12 || string(b[8:20]) != string(transaction) {
		return nil, errors.New("invalid STUN response")
	}
	for off := 20; off+4 <= len(b); {
		typ := binary.BigEndian.Uint16(b[off : off+2])
		n := int(binary.BigEndian.Uint16(b[off+2 : off+4]))
		off += 4
		if off+n > len(b) {
			break
		}
		if typ == 0x20 && n == 8 && b[off+1] == 1 {
			ip := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				ip[i] = b[off+4+i] ^ b[4+i]
			}
			return &net.UDPAddr{IP: ip, Port: int(binary.BigEndian.Uint16(b[off+2:off+4]) ^ uint16(Magic>>16))}, nil
		}
		off += (n + 3) &^ 3
	}
	return nil, errors.New("missing IPv4 XOR-MAPPED-ADDRESS")
}
