package okcptun

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"github.com/zeebo/blake3"
)

type KCPMux struct {
	baseConn net.PacketConn
	key      [32]byte
	cipher   cipher.Block
	isServer bool
	conns    map[uint32]*KCPMuxConn
	chConns  chan *KCPMuxConn
	mutex    sync.Mutex
}

type KCPMuxConn struct {
	mux        *KCPMux
	connId     uint32
	closed     bool
	chPackets  chan *packet
	remoteAddr net.Addr
}

type packet struct {
	buffer []byte
	addr   net.Addr
}

var (
	flagFast          = flag.Int("fast", 2, "")
	flagMinPacketSize = flag.Int("minPacketSize", 70, "")
	flagMaxPacketSize = flag.Int("maxPacketSize", 1252, "")
)

func NewKCPMux(conn net.PacketConn, password string, isServer bool) *KCPMux {
	mux := &KCPMux{}
	mux.baseConn = conn
	blake3.DeriveKey("okcptun", []byte(password), mux.key[:])
	mux.cipher, _ = aes.NewCipher(mux.key[:])
	mux.isServer = isServer
	mux.conns = make(map[uint32]*KCPMuxConn)
	mux.chConns = make(chan *KCPMuxConn, 128)
	go mux.readLoop()
	return mux
}

func (mux *KCPMux) Accept() (*kcp.UDPSession, io.Closer) {
	baseConn := <-mux.chConns
	conn, _ := kcp.NewConn3(
		baseConn.connId, baseConn.remoteAddr, nil, 0, 0, baseConn)
	configureKCPConn(conn)
	return conn, baseConn
}

func (mux *KCPMux) Dial(remoteAddr *net.UDPAddr) (*kcp.UDPSession, io.Closer) {
	mux.mutex.Lock()
	defer mux.mutex.Unlock()

	baseConn := &KCPMuxConn{}
	baseConn.mux = mux
	for {
		binary.Read(rand.Reader, binary.LittleEndian, &baseConn.connId)
		_, ok := mux.conns[baseConn.connId]
		if !ok {
			break
		}
	}
	baseConn.chPackets = make(chan *packet, 1024)
	mux.conns[baseConn.connId] = baseConn

	conn, _ := kcp.NewConn3(baseConn.connId, remoteAddr, nil, 0, 0, baseConn)
	configureKCPConn(conn)
	return conn, baseConn
}

func (mux *KCPMux) readLoop() {
	for {
		buffer := [1500]byte{}
		size, addr, err := mux.baseConn.ReadFrom(buffer[:])
		if err != nil {
			log.Fatal("KCPMux.readLoop: ReadFrom failed: ", err)
		}
		if size < 20 {
			continue
		}
		stream := cipher.NewCTR(mux.cipher, buffer[size-16:size])
		stream.XORKeyStream(buffer[:size-16], buffer[:size-16])
		hasher, _ := blake3.NewKeyed(mux.key[:])
		hasher.Write(buffer[:size-16])
		digest := [16]byte{}
		hasher.Digest().Read(digest[:])
		if !bytes.Equal(buffer[size-16:size], digest[:]) {
			continue
		}
		connId := binary.LittleEndian.Uint32(buffer[:4])
		mux.dispatch(connId, buffer[:size-16], addr)
	}
}

func (mux *KCPMux) dispatch(connId uint32, p []byte, addr net.Addr) {
	mux.mutex.Lock()
	defer mux.mutex.Unlock()

	conn, ok := mux.conns[connId]
	if !ok {
		if !mux.isServer {
			// TODO: Send RST.
			log.Print("KCPMux.dispatch: unknown connection: ", connId)
			return
		}
		conn = &KCPMuxConn{}
		conn.mux = mux
		conn.connId = connId
		conn.chPackets = make(chan *packet, 1024)
		conn.remoteAddr = addr
		select {
		case mux.chConns <- conn:
		default:
			return
		}
		mux.conns[connId] = conn
	}
	if conn.closed {
		return
	}
	packet := &packet{p, addr}
	select {
	case conn.chPackets <- packet:
	default:
	}
}

func (conn *KCPMuxConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if conn.closed {
		return 0, nil, io.EOF
	}
	packet := <-conn.chPackets
	if packet == nil {
		return 0, nil, io.EOF
	}
	size := copy(p, packet.buffer)
	return size, packet.addr, nil
}

func (conn *KCPMuxConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if conn.closed {
		return 0, io.EOF
	}
	if len(p)+16 < *flagMinPacketSize {
		p = append(p, make([]byte, *flagMinPacketSize-len(p)-16)...)
	}
	hasher, _ := blake3.NewKeyed(conn.mux.key[:])
	hasher.Write(p)
	p = append(p, make([]byte, 16)...)
	hasher.Digest().Read(p[len(p)-16:])
	stream := cipher.NewCTR(conn.mux.cipher, p[len(p)-16:])
	stream.XORKeyStream(p[:len(p)-16], p[:len(p)-16])
	return conn.mux.baseConn.WriteTo(p, addr)
}

func (conn *KCPMuxConn) Close() error {
	if conn.closed {
		return nil
	}
	conn.closed = true
	select {
	case conn.chPackets <- nil:
	default:
	}
	go func() {
		time.Sleep(time.Second * 30)

		mux := conn.mux
		mux.mutex.Lock()
		defer mux.mutex.Unlock()
		delete(mux.conns, conn.connId)
	}()
	return nil
}

func (conn *KCPMuxConn) LocalAddr() net.Addr {
	return conn.mux.baseConn.LocalAddr()
}

func (conn *KCPMuxConn) RemoteAddr() net.Addr {
	return conn.remoteAddr
}

func (conn *KCPMuxConn) SetDeadline(t time.Time) error {
	log.Print("KCPMuxConn.SetDeadline not implemented")
	return nil
}

func (conn *KCPMuxConn) SetReadDeadline(t time.Time) error {
	log.Print("KCPMuxConn.SetReadDeadline not implemented")
	return nil
}

func (conn *KCPMuxConn) SetWriteDeadline(t time.Time) error {
	log.Print("KCPMuxConn.SetWriteDeadline not implemented")
	return nil
}

func configureKCPConn(conn *kcp.UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWriteDelay(false)
	switch *flagFast {
	case 0:
		conn.SetNoDelay(0, 40, 2, 1)
	case 1:
		conn.SetNoDelay(0, 30, 2, 1)
	case 2:
		conn.SetNoDelay(1, 20, 2, 1)
	case 3:
		conn.SetNoDelay(1, 10, 2, 1)
	default:
		log.Fatal("invalid fast: ", *flagFast)
	}
	conn.SetWindowSize(1024, 1024)
	conn.SetACKNoDelay(false)
	conn.SetMtu(*flagMaxPacketSize - 16)
}
