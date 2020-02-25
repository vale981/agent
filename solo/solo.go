package solo

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
	setBLOBVector = []byte("<setBLOBVector")
)

type INDIHubSoloTunnel interface {
	Send(response *indihub.Response) error
	CloseAndRecv() (*indihub.SoloSummary, error)
}

type SoloTcpProxy struct {
	Name     string
	Addr     string
	listener net.Listener
	Tunnel   INDIHubSoloTunnel

	connInMap  map[uint32]net.Conn
	connOutMap map[uint32]net.Conn
	shouldExit int32
}

func New(name string, addr string, tunnel INDIHubSoloTunnel) *SoloTcpProxy {
	return &SoloTcpProxy{
		Name:       name,
		Addr:       addr,
		Tunnel:     tunnel,
		connInMap:  map[uint32]net.Conn{},
		connOutMap: map[uint32]net.Conn{},
	}
}

func (p *SoloTcpProxy) Start(addr string, sessionID uint64, sessionToken string) {
	log.Printf("Starting INDI-server for INDIHUB in solo mode on %s ...", addr)
	var err error
	p.listener, err = net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Could not start INDI-server for INDIHUB in solo mode: %v\n", err)
		return
	}
	log.Println("...OK")

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
			_, err := io.Copy(connOut, connIn)
			if err == nil {
				// client closed connection so closing to INDI-server
				connOut.Close()
			} else {
				connIn.Close()
				connOut.Close()
			}
			delete(p.connInMap, cNum)
			delete(p.connOutMap, cNum)
		}(connCnt)

		// copy responses
		go func(cNum uint32, sessID uint64, sessToken string) {
			buf := make([]byte, lib.INDIServerMaxSendMsgSize, lib.INDIServerMaxSendMsgSize)
			xmlFlattener := lib.NewXmlFlattener()
			for {
				n, err := connOut.Read(buf)
				if err != nil {
					log.Printf("%s - could not read from INDI-server: %v\n", p.Name, err)
					connIn.Close()
					return
				}

				if _, err := connIn.Write(buf[:n]); err != nil {
					log.Printf("%s - could not write to INDI-client: %v\n", p.Name, err)
					connOut.Close()
					return
				}

				// catch images
				xmlCommands := xmlFlattener.FeedChunk(buf[:n])
				for _, xmlComm := range xmlCommands {
					if !bytes.HasPrefix(xmlComm, setBLOBVector) {
						continue
					}
					err := p.sendImages(xmlComm, cNum, sessID, sessToken)
					if err != nil {
						log.Printf("%s - could not send image to INDIHUB: %v\n", p.Name, err)
					}
				}
			}
		}(connCnt, sessionID, sessionToken)
	}
	// close connections to tunnel
	if summary, err := p.Tunnel.CloseAndRecv(); err == nil {
		gc := color.New(color.FgGreen)
		gc.Println()
		gc.Println("                                ************************************************************")
		gc.Println("                                *              INDIHUB solo session finished!!             *")
		gc.Println("                                ************************************************************")
		gc.Println("                                                                                            ")

		gc.Printf("                                   "+
			"Processed %d images. Thank you for your contribution!\n",
			summary.ImagesNum,
		)
		gc.Println("                                ************************************************************")
	} else {
		log.Printf("Error getting solo-session summary: %v", err)
	}
}

func (p *SoloTcpProxy) Close() {
	atomic.SwapInt32(&p.shouldExit, 1)
	for _, c := range p.connInMap {
		c.Close()
	}
	p.listener.Close()
	for _, c := range p.connOutMap {
		c.Close()
	}
}

func (p *SoloTcpProxy) sendImages(imagesData []byte, cNum uint32, sessID uint64, sessToken string) error {
	resp := &indihub.Response{
		Data:         imagesData,
		Conn:         cNum,
		SessionID:    sessID,
		SessionToken: sessToken,
	}

	return p.Tunnel.Send(resp)
}
