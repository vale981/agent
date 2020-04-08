package proxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/indihub-space/agent/hostutils"
	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/proto/indihub"
)

const queueSize = 4096

type INDIHubTunnel interface {
	Send(*indihub.Response) error
	Recv() (*indihub.Request, error)
	CloseSend() error
}

type TcpProxy struct {
	Name   string
	Addr   string
	Tunnel INDIHubTunnel

	connMu  sync.Mutex
	connMap map[uint32]net.Conn

	filter *hostutils.INDIFilter

	respPool *sync.Pool
}

type PublicServerAddr struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

func New(name string, addr string, tunnel INDIHubTunnel, filter *hostutils.INDIFilter) *TcpProxy {
	return &TcpProxy{
		Name:    name,
		Addr:    addr,
		Tunnel:  tunnel,
		connMap: map[uint32]net.Conn{},
		filter:  filter,
		respPool: &sync.Pool{
			New: func() interface{} {
				return &indihub.Response{
					Data: make([]byte, lib.INDIServerMaxRecvMsgSize),
				}
			},
		},
	}
}

func (p *TcpProxy) Close() {
	p.connMu.Lock()
	defer p.connMu.Unlock()

	for num, c := range p.connMap {
		c.Close()
		delete(p.connMap, num)
	}
}

func (p *TcpProxy) connect(cNum uint32) (net.Conn, bool, error) {
	p.connMu.Lock()
	defer p.connMu.Unlock()

	if c, ok := p.connMap[cNum]; ok {
		return c, false, nil
	}

	log.Printf("Connecting to local %s... on %s\n", p.Name, p.Addr)
	c, err := net.Dial("tcp", p.Addr)
	if err != nil {
		log.Printf("Could not connect to %s: %s\n", p.Name, err)
		return nil, false, err
	}
	log.Println("...OK")
	p.connMap[cNum] = c
	return c, true, err
}

func (p *TcpProxy) reConnect(cNum uint32) (net.Conn, error) {
	p.connMu.Lock()
	defer p.connMu.Unlock()

	log.Println("Re-connecting to local %s... on %s\n", p.Name, p.Addr)
	c, err := net.Dial("tcp", p.Addr)
	if err != nil {
		log.Printf("Could not connect to %s: %s\n", p.Name, err)
		return nil, err
	}
	log.Println("...OK")
	p.connMap[cNum] = c
	return c, err
}

func (p *TcpProxy) close(cNum uint32) {
	p.connMu.Lock()
	defer p.connMu.Unlock()

	if c, ok := p.connMap[cNum]; ok {
		c.Close()
		delete(p.connMap, cNum)
	}
}

func (p *TcpProxy) Start(pubAddrChan chan PublicServerAddr, sessionID uint64, sessionToken string) {
	wg := sync.WaitGroup{}

	// run response sending queue
	respCh := make(chan *indihub.Response, queueSize)
	defer close(respCh)
	go p.sendResponses(respCh)

	addrReceived := false
	xmlFlattener := map[uint32]*lib.XmlFlattener{}
	for {
		// receive request from tunnel
		in, err := p.Tunnel.Recv()
		if err == io.EOF {
			// read done, server closed connection
			log.Printf("Exiting. Got EOF from %s tunnel.\n", p.Name)
			break
		}
		if err != nil {
			log.Printf("Exiting. Failed to receive a request from %s tunnel: %v\n", p.Name, err)
			break
		}

		// 1st message always with server address
		if !addrReceived && in.Conn == 0 {
			pubAddrChan <- PublicServerAddr{
				Name: p.Name,
				Addr: string(in.Data),
			}
			addrReceived = true
			continue
		}

		// Flatten XML data stream into elements
		if xmlFlattener[in.Conn] == nil {
			xmlFlattener[in.Conn] = lib.NewXmlFlattener()
		}

		xmlCommands := xmlFlattener[in.Conn].FeedChunk(in.Data)

		// check if we need to filter traffic
		if p.filter != nil {
			xmlCommands = p.filter.FilterIncoming(xmlCommands)
		}

		c, isNewConn, err := p.connect(in.Conn)
		if err != nil {
			if c, err = p.reConnect(in.Conn); err != nil {
				log.Printf("Failed to send a request to %s: %v\n", p.Name, err)
				time.Sleep(1 * time.Second)
				continue
			}
		}

		if in.Closed {
			log.Printf("Client closed connection %d to the cloud, so closing it to local %s too\n",
				in.Conn, p.Name)
			p.close(in.Conn)
			continue
		}

		if isNewConn {
			// INDI Server responses
			wg.Add(1)
			go func(conn net.Conn, cNum uint32, sessID uint64, sessToken string, ch chan *indihub.Response) {
				defer wg.Done()
				readBuf := make([]byte, lib.INDIServerMaxRecvMsgSize)
				for {
					// receive response from server
					n, err := conn.Read(readBuf)
					if err == io.EOF {
						if conn, err = p.reConnect(cNum); err != nil {
							log.Printf("Failed to send a request to %s: %v", p.Name, err)
							time.Sleep(1 * time.Second)
							continue
						} else {
							n, err = conn.Read(readBuf)
						}
					}
					if err != nil {
						log.Printf("Failed to receive a response from %s: %v", p.Name, err)
						return
					}

					// send response to tunnel
					resp := p.respPool.Get().(*indihub.Response)
					resp.Conn = cNum
					resp.SessionToken = sessionToken
					resp.SessionID = sessionID
					resp.Data = resp.Data[:n]
					copy(resp.Data, readBuf[:n])
					ch <- resp
				}
			}(c, in.Conn, sessionID, sessionToken, respCh)
		}

		if len(xmlCommands) == 0 {
			continue
		}

		for _, command := range xmlCommands {
			if _, err = c.Write(command); err == io.EOF {
				if c, err = p.reConnect(in.Conn); err != nil {
					log.Printf("Failed to send a request to %s: %v", p.Name, err)
					time.Sleep(1 * time.Second)
					continue
				} else {
					_, err = c.Write(command)
				}
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			log.Printf("Failed to send a request to %s: %v", p.Name, err)
			time.Sleep(1 * time.Second)
			continue
		}
	}
	wg.Wait()
}

func (p *TcpProxy) sendResponses(respCh chan *indihub.Response) {
	for resp := range respCh {
		if err := p.Tunnel.Send(resp); err != nil {
			log.Printf("Failed to send a response to %s tunnel: %v", p.Name, err)
		}
		p.respPool.Put(resp)
	}
}
