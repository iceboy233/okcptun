package okcptun

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

type KCPMux struct {
	baseConn net.PacketConn
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

// TODO: Enlarge the limit
const MaxConns = 128

func NewKCPMux(conn net.PacketConn) *KCPMux {
	mux := &KCPMux{}
	mux.baseConn = conn
	mux.conns = make(map[uint32]*KCPMuxConn)
	mux.chConns = make(chan *KCPMuxConn)
	go mux.readLoop()
	return mux
}

func (mux *KCPMux) Accept() (*kcp.UDPSession, error) {
	baseConn := <-mux.chConns
	conn, err := kcp.NewConn3(
		baseConn.connId, baseConn.remoteAddr, nil, 0, 0, baseConn)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (mux *KCPMux) Dial(remoteAddr *net.UDPAddr) (*kcp.UDPSession, error) {
	mux.mutex.Lock()
	defer mux.mutex.Unlock()

	if len(mux.conns) >= MaxConns {
		return nil, errors.New("too many connections")
	}
	baseConn := &KCPMuxConn{}
	baseConn.mux = mux
	for {
		binary.Read(rand.Reader, binary.LittleEndian, &baseConn.connId)
		_, ok := mux.conns[baseConn.connId]
		if !ok {
			break
		}
	}
	baseConn.chPackets = make(chan *packet)
	mux.conns[baseConn.connId] = baseConn

	conn, err := kcp.NewConn3(baseConn.connId, remoteAddr, nil, 0, 0, baseConn)
	if err != nil {
		log.Fatal("KCPMux.Dial: NewConn failed: ", err)
	}
	return conn, nil
}

func (mux *KCPMux) readLoop() {
	for {
		buffer := [1500]byte{}
		size, addr, err := mux.baseConn.ReadFrom(buffer[:])
		if err != nil {
			log.Fatal("KCPMux.readLoop: ReadFrom failed: ", err)
		}
		if size < 4 {
			continue
		}
		connId := binary.LittleEndian.Uint32(buffer[:4])
		mux.dispatch(connId, buffer[:], addr)
	}
}

func (mux *KCPMux) dispatch(connId uint32, p []byte, addr net.Addr) {
	mux.mutex.Lock()
	defer mux.mutex.Unlock()

	conn, ok := mux.conns[connId]
	if !ok {
		if len(mux.conns) >= MaxConns {
			log.Print("KCPMux.dispatch: too many connections")
			return
		}
		conn = &KCPMuxConn{}
		conn.mux = mux
		conn.connId = connId
		conn.chPackets = make(chan *packet)
		conn.remoteAddr = addr
		mux.conns[connId] = conn
		mux.chConns <- conn
	}
	packet := &packet{p, addr}
	conn.chPackets <- packet
}

func (conn *KCPMuxConn) ReadFrom(p []byte) (int, net.Addr, error) {
	packet := <-conn.chPackets
	size := copy(p, packet.buffer)
	return size, packet.addr, nil
}

func (conn *KCPMuxConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return conn.mux.baseConn.WriteTo(p, addr)
}

func (conn *KCPMuxConn) Close() error {
	log.Print("KCPMuxConn.Close not implemented")
	conn.closed = true
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
