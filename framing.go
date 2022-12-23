package okcptun

import (
	"encoding/binary"
	"io"
)

func WrapFrames(dst io.Writer, src io.Reader) {
	buffer := [65538]byte{}
	for {
		size, err := src.Read(buffer[2:])
		if err != nil {
			dst.Write([]byte{0, 0})
			return
		}
		binary.BigEndian.PutUint16(buffer[0:2], uint16(size))
		_, err = dst.Write(buffer[:size+2])
		if err != nil {
			return
		}
	}
}

func UnwrapFrames(dst io.WriteCloser, src io.Reader) {
	defer dst.Close()

	buffer := [65536]byte{}
	for {
		_, err := io.ReadFull(src, buffer[:2])
		if err != nil {
			return
		}
		frameSize := int(binary.BigEndian.Uint16(buffer[:2]))
		if frameSize == 0 {
			return
		}
		for frameSize > 0 {
			size, err := src.Read(buffer[:frameSize])
			if err != nil {
				return
			}
			_, err = dst.Write(buffer[:size])
			if err != nil {
				return
			}
			frameSize -= size
		}
	}
}