package okcptun

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"hash"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/hkdf"
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
	closeOnce  sync.Once
	chPackets  chan *packet
	remoteAddr net.Addr
}

type packet struct {
	buffer []byte
	addr   net.Addr
}

var (
	flagFast          = flag.Int("fast", 2, "")
	flagMinPacketSize = flag.Int("minPacketSize", 64, "")
	flagMaxPacketSize = flag.Int("maxPacketSize", 1252, "")
	flagStreamMode    = flag.Bool("streamMode", true, "")
	flagWriteDelay    = flag.Bool("writeDelay", false, "")
	flagSendWindow    = flag.Int("sendWindow", 1024, "")
	flagReceiveWindow = flag.Int("receiveWindow", 1024, "")
	flagACKNoDelay    = flag.Bool("ackNoDelay", false, "")
)

func NewKCPMux(conn net.PacketConn, password string, isServer bool) *KCPMux {
	mux := &KCPMux{}
	mux.baseConn = conn
	hkdf := hkdf.New(
		func() hash.Hash { hash, _ := blake2b.New256(nil); return hash },
		[]byte(password), []byte("okcptun"), nil)
	hkdf.Read(mux.key[:])
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
		hash, _ := blake2b.New(16, mux.key[:])
		hash.Write(buffer[:size-16])
		sum := hash.Sum(nil)
		if !bytes.Equal(buffer[size-16:size], sum) {
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
	packet := &packet{p, addr}
	select {
	case conn.chPackets <- packet:
	default:
	}
}

func (conn *KCPMuxConn) ReadFrom(p []byte) (int, net.Addr, error) {
	packet := <-conn.chPackets
	if packet == nil {
		return 0, nil, io.EOF
	}
	size := copy(p, packet.buffer)
	return size, packet.addr, nil
}

func (conn *KCPMuxConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if len(p)+16 < *flagMinPacketSize {
		p = append(p, make([]byte, *flagMinPacketSize-len(p)-16)...)
	}
	hash, _ := blake2b.New(16, conn.mux.key[:])
	hash.Write(p)
	p = hash.Sum(p)
	stream := cipher.NewCTR(conn.mux.cipher, p[len(p)-16:])
	stream.XORKeyStream(p[:len(p)-16], p[:len(p)-16])
	return conn.mux.baseConn.WriteTo(p, addr)
}

func (conn *KCPMuxConn) Close() error {
	conn.closeOnce.Do(func() {
		conn.mux.mutex.Lock()
		delete(conn.mux.conns, conn.connId)
		conn.mux.mutex.Unlock()
		close(conn.chPackets)
	})
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
	conn.SetStreamMode(*flagStreamMode)
	conn.SetWriteDelay(*flagWriteDelay)
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
	conn.SetWindowSize(*flagSendWindow, *flagReceiveWindow)
	conn.SetACKNoDelay(*flagACKNoDelay)
	conn.SetMtu(*flagMaxPacketSize - 16)
}
