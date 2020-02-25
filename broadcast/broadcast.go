package broadcast

import (
	"bytes"
	"io"
	"log"
	"net"
	"sync/atomic"

	"github.com/fatih/color"

	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/proto/indihub"
)

var (
	getPropertiesStart = []byte("<getProperties")
)

type INDIHubBroadcastTunnel interface {
	Send(*indihub.Response) error
	Recv() (*indihub.Request, error)
	CloseSend() error
}

type BroadcastTcpProxy struct {
	Name     string
	Addr     string
	listener net.Listener
	Tunnel   INDIHubBroadcastTunnel

	connInMap  map[uint32]net.Conn
	connOutMap map[uint32]net.Conn
	shouldExit int32
}

func New(name string, addr string, tunnel INDIHubBroadcastTunnel) *BroadcastTcpProxy {
	return &BroadcastTcpProxy{
		Name:       name,
		Addr:       addr,
		Tunnel:     tunnel,
		connInMap:  map[uint32]net.Conn{},
		connOutMap: map[uint32]net.Conn{},
	}
}

func (p *BroadcastTcpProxy) Start(sessionID uint64, sessionToken string, addr string) {
	log.Printf("Starting INDI-server for INDIHUB in broadcast mode on %s ...", addr)
	var err error
	p.listener, err = net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Could not start INDI-server for INDIHUB in broadcast mode: %v\n", err)
		return
	}
	log.Println("...OK")

	// receive public address from tunnel
	in, err := p.Tunnel.Recv()
	if err == io.EOF {
		// read done, server closed connection
		log.Printf("Exiting. Got EOF from %s tunnel.\n", p.Name)
		return
	}
	if err != nil {
		log.Printf("Exiting. Failed to receive a request from %s tunnel: %v\n", p.Name, err)
		return
	}

	c := color.New(color.FgCyan)
	gc := color.New(color.FgGreen)
	c.Println()
	c.Println("                                ************************************************************")
	c.Println("                                *                INDIHUB broadcast address !!              *")
	c.Println("                                ************************************************************")
	c.Println("                                                                                            ")
	gc.Printf("                                              %s\n", string(in.Data))
	c.Println("                                                                                            ")
	c.Println("                                ************************************************************")
	c.Println()

	var connCnt uint32

	for {
		if atomic.LoadInt32(&p.shouldExit) == 1 {
			break
		}

		// Wait for a connection from INDI-client
		connIn, err := p.listener.Accept()
		if err != nil {
			break
		}

		// connection to real INDI-server
		connOut, err := net.Dial("tcp", p.Addr)
		if err != nil {
			log.Printf("%s - could not connect to INDI-server: %v\n", p.Name, err)
			connIn.Close()
			continue
		}

		connCnt += 1
		p.connInMap[connCnt] = connIn
		p.connOutMap[connCnt] = connOut

		// copy requests
		go func(cNum uint32) {
			buf := make([]byte, lib.INDIServerMaxRecvMsgSize, lib.INDIServerMaxRecvMsgSize)
			for {
				// read from client
				n, err := connIn.Read(buf)
				if err != nil {
					log.Printf("%s - could not read from INDI-client: %v\n", p.Name, err)
					connIn.Close()
					connOut.Close()
					break
				}

				// we want to let server know about getProperties and connection number
				if bytes.HasPrefix(buf[:n], getPropertiesStart) {
					// broadcast
					resp := &indihub.Response{
						Data:         buf[:n],
						Conn:         cNum,
						SessionID:    sessionID,
						SessionToken: sessionToken,
					}
					if err := p.Tunnel.Send(resp); err != nil {
						log.Printf("Failed broadcast request to %s tunnel: %v", p.Name, err)
					}
				}

				// send to server
				if _, err := connOut.Write(buf[:n]); err != nil {
					log.Printf("%s - could not write to INDI-server: %v\n", p.Name, err)
					connIn.Close()
					connOut.Close()
					break
				}
			}
			delete(p.connInMap, cNum)
			delete(p.connOutMap, cNum)
		}(connCnt)

		// copy and broadcast responses
		go func(cNum uint32, sessID uint64) {
			buf := make([]byte, lib.INDIServerMaxSendMsgSize, lib.INDIServerMaxSendMsgSize)
			for {
				n, err := connOut.Read(buf)
				if err != nil {
					log.Printf("%s - could not read from INDI-server: %v\n", p.Name, err)
					connIn.Close()
					return
				}

				// send to client
				if _, err := connIn.Write(buf[:n]); err != nil {
					log.Printf("%s - could not write to INDI-client: %v\n", p.Name, err)
					connOut.Close()
					return
				}

				// broadcast
				resp := &indihub.Response{
					Data:         buf[:n],
					Conn:         cNum,
					SessionID:    sessID,
					SessionToken: sessionToken,
				}
				if err := p.Tunnel.Send(resp); err != nil {
					log.Printf("Failed broadcast response to %s tunnel: %v", p.Name, err)
				}
			}
		}(connCnt, sessionID)
	}
	// close connections to tunnel

}

func (p *BroadcastTcpProxy) Close() {
	atomic.SwapInt32(&p.shouldExit, 1)
	for _, c := range p.connInMap {
		c.Close()
	}
	p.listener.Close()
	for _, c := range p.connOutMap {
		c.Close()
	}
}
