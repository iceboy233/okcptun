package okcptun

import (
	"encoding/binary"
	"flag"
	"io"
	"net"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

var (
	flagTimeout = flag.Int("timeout", 30, "")
)

func WrapFrames(dst *kcp.UDPSession, src *net.TCPConn) {
	buffer := [65538]byte{}
	for {
		src.SetReadDeadline(computeDeadline())
		size, err := src.Read(buffer[2:])
		if err != nil {
			dst.SetWriteDeadline(computeDeadline())
			dst.Write([]byte{0, 0})
			return
		}
		binary.BigEndian.PutUint16(buffer[0:2], uint16(size))
		dst.SetWriteDeadline(computeDeadline())
		_, err = dst.Write(buffer[:size+2])
		if err != nil {
			return
		}
	}
}

func UnwrapFrames(dst *net.TCPConn, src *kcp.UDPSession) {
	defer dst.Close()

	buffer := [65536]byte{}
	for {
		src.SetReadDeadline(computeDeadline())
		_, err := io.ReadFull(src, buffer[:2])
		if err != nil {
			return
		}
		frameSize := int(binary.BigEndian.Uint16(buffer[:2]))
		if frameSize == 0 {
			return
		}
		for frameSize > 0 {
			src.SetReadDeadline(computeDeadline())
			size, err := src.Read(buffer[:frameSize])
			if err != nil {
				return
			}
			dst.SetWriteDeadline(computeDeadline())
			_, err = dst.Write(buffer[:size])
			if err != nil {
				return
			}
			frameSize -= size
		}
	}
}

func computeDeadline() time.Time {
	return time.Now().Add(time.Second * time.Duration(*flagTimeout))
}
