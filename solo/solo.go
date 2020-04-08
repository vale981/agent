package solo

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/clbanning/mxj"
	"github.com/fatih/color"

	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/proto/indihub"
)

const queueSize = 4096

var (
	getProperties    = []byte("<getProperties version='1.7'/>")
	getCCDProperties = "<getProperties device='%s' version='1.7'/>"
	enableBLOBNever  = "<enableBLOB device='%s'>Never</enableBLOB>"
	enableBLOBOnly   = "<enableBLOB device='%s'>Only</enableBLOB>"

	setBLOBVector   = []byte("<setBLOBVector")
	defNumberVector = []byte("<defNumberVector")
)

type INDIHubSoloTunnel interface {
	Send(response *indihub.Response) error
	CloseAndRecv() (*indihub.SoloSummary, error)
}

type Agent struct {
	indiServerAddr string
	indiConn       net.Conn
	tunnel         INDIHubSoloTunnel
	shouldExit     bool

	ccdConnMap   map[string]net.Conn
	ccdConnMapMu sync.Mutex

	sessionID    uint64
	sessionToken string

	respPool *sync.Pool
}

func New(indiServerAddr string, tunnel INDIHubSoloTunnel) *Agent {
	return &Agent{
		indiServerAddr: indiServerAddr,
		tunnel:         tunnel,
		ccdConnMap:     make(map[string]net.Conn),
		respPool: &sync.Pool{
			New: func() interface{} {
				return &indihub.Response{
					Data: make([]byte, lib.INDIServerMaxRecvMsgSize),
				}
			},
		},
	}
}

func (p *Agent) Start(sessionID uint64, sessionToken string) error {
	p.sessionID = sessionID
	p.sessionToken = sessionToken

	// open connection to real INDI-server
	var err error
	p.indiConn, err = p.connectToINDI()
	if err != nil {
		log.Printf("could not connect to INDI-server in solo-mode: %s\n", err)
		return err
	}
	defer p.indiConn.Close()

	// run response sending queue
	respCh := make(chan *indihub.Response, queueSize)
	defer close(respCh)
	go p.sendResponses(respCh)

	// listen INDI-server for data
	buf := make([]byte, lib.INDIServerMaxSendMsgSize)
	xmlFlattener := lib.NewXmlFlattener()
	wg := sync.WaitGroup{}
	var connNum uint32
	for {
		if p.shouldExit {
			break
		}

		n, err := p.indiConn.Read(buf)
		if err == io.EOF {
			// reconnect
			if p.indiConn, err = p.connectToINDI(); err != nil {
				log.Printf("Failed to re-connect to INDI-server is solo mode: %s", err)
				break
			} else {
				n, err = p.indiConn.Read(buf)
			}
		}
		if err != nil {
			log.Println("could not read from INDI-server in solo-mode:", err)
			break
		}

		// subscribe to BLOBs and catch images
		xmlCommands := xmlFlattener.FeedChunk(buf[:n])

		for _, xmlCmd := range xmlCommands {
			// subscribe to BLOBs from CCDs by catching defNumberVector property with name="CCD_EXPOSURE"
			if !bytes.HasPrefix(xmlCmd, defNumberVector) {
				continue
			}

			mapVal, err := mxj.NewMapXml(xmlCmd, true)
			if err != nil {
				log.Println("could not parse XML chunk in solo-mode:", err)
				continue
			}
			defNumberVectorMap, _ := mapVal.ValueForKey("defNumberVector")
			if defNumberVectorMap == nil {
				continue
			}
			defNumberVectorVal := defNumberVectorMap.(map[string]interface{})

			if nameStr, ok := defNumberVectorVal["attr_name"].(string); ok && nameStr == "CCD_EXPOSURE" {
				if deviceStr, ok := defNumberVectorVal["attr_device"].(string); ok && p.getConnCCD(deviceStr) == nil {
					// disable receiving BLOBs on main connection
					if _, err := p.indiConn.Write([]byte(fmt.Sprintf(enableBLOBNever, deviceStr))); err != nil {
						log.Printf("could not write to INDI-server in solo-mode: %s\n", err)
					}

					// launch Go-routine with connection per CCD and only BLOB enabled
					connNum++
					wg.Add(1)
					go func(ccdName string, cNum uint32, ch chan *indihub.Response) {
						defer wg.Done()
						p.readFromCCD(ccdName, cNum, ch)
					}(deviceStr, connNum, respCh)
				}
			}
		}
	}

	wg.Wait()

	// close connections to tunnel
	if summary, err := p.tunnel.CloseAndRecv(); err == nil {
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

	return nil
}

func (p *Agent) connectToINDI() (net.Conn, error) {
	log.Println("Connecting to INDI-server in solo mode...")
	conn, err := net.Dial("tcp", p.indiServerAddr)
	if err != nil {
		log.Printf("could not connect to INDI-server in solo-mode: %s\n", err)
		return nil, err
	}

	// set connection to receive data
	if _, err := conn.Write(getProperties); err != nil {
		log.Printf("could not write to INDI-server in solo-mode: %s\n", err)
		conn.Close()
		return nil, err
	}

	// disable BLOBs for CCDs if any
	names := p.getCurrCCD()
	for _, ccdName := range names {
		if _, err := conn.Write([]byte(fmt.Sprintf(enableBLOBNever, ccdName))); err != nil {
			log.Printf("could not write to INDI-server in solo-mode: %s\n", err)
		}
	}

	log.Println("...OK")

	return conn, nil
}

func (p *Agent) getCurrCCD() []string {
	p.ccdConnMapMu.Lock()
	defer p.ccdConnMapMu.Unlock()
	names := []string{}
	for ccdName := range p.ccdConnMap {
		names = append(names, ccdName)
	}

	return names
}

func (p *Agent) getConnCCD(ccdName string) net.Conn {
	p.ccdConnMapMu.Lock()
	defer p.ccdConnMapMu.Unlock()
	return p.ccdConnMap[ccdName]
}

func (p *Agent) setConnCCD(ccdName string, conn net.Conn) {
	p.ccdConnMapMu.Lock()
	defer p.ccdConnMapMu.Unlock()
	p.ccdConnMap[ccdName] = conn
}

func (p *Agent) readFromCCD(ccdName string, cNum uint32, ch chan *indihub.Response) {
	// open connection
	conn, err := p.connectToCCD(ccdName)
	if err != nil {
		return
	}
	defer conn.Close()

	// read data from INDI-server and send to tunnel
	buf := make([]byte, lib.INDIServerMaxSendMsgSize)
	for {
		if p.shouldExit {
			break
		}

		n, err := conn.Read(buf)
		if err == io.EOF {
			// reconnect
			if conn, err = p.connectToCCD(ccdName); err != nil {
				log.Printf("Failed to re-connect to INDI-server for %s is solo mode: %s\n", ccdName, err)
				break
			} else {
				n, err = conn.Read(buf)
			}
		}
		if err != nil {
			log.Printf("could not read from INDI-server for %s in solo-mode: %s\n", ccdName, err)
			break
		}

		// send data to tunnel
		resp := p.respPool.Get().(*indihub.Response)
		resp.Conn = cNum
		resp.SessionToken = p.sessionToken
		resp.SessionID = p.sessionID
		resp.Data = resp.Data[:n]
		copy(resp.Data, buf[:n])
		ch <- resp
	}
}

func (p *Agent) sendResponses(respCh chan *indihub.Response) {
	for resp := range respCh {
		if err := p.tunnel.Send(resp); err != nil {
			log.Printf("Failed to send a response to tunnel in solo-mode: %s", err)
		}
		p.respPool.Put(resp)
	}
}

func (p *Agent) connectToCCD(ccdName string) (net.Conn, error) {
	// open connection
	log.Println("Connecting to INDI-device:", ccdName)
	conn, err := net.Dial("tcp", p.indiServerAddr)
	if err != nil {
		log.Printf("could not connect to INDI-server for CCD '%s' in solo-mode: %s\n",
			ccdName, err)
		return nil, err
	}
	log.Println("...OK")

	// set connection to receive data
	if _, err := conn.Write([]byte(fmt.Sprintf(getCCDProperties, ccdName))); err != nil {
		log.Printf("getProperties: could not write to INDI-server for %s in solo-mode: %s\n", ccdName, err)
		conn.Close()
		return nil, err
	}

	// enable BLOBS only
	if _, err := conn.Write([]byte(fmt.Sprintf(enableBLOBOnly, ccdName))); err != nil {
		log.Printf("getProperties: could not write to INDI-server for %s in solo-mode: %s\n", ccdName, err)
		conn.Close()
		return nil, err
	}

	p.setConnCCD(ccdName, conn)

	return conn, nil
}

func (p *Agent) Close() {
	// close connections to CCDs
	p.ccdConnMapMu.Lock()
	defer p.ccdConnMapMu.Unlock()
	for ccdName, conn := range p.ccdConnMap {
		log.Println("Closing connection to:", ccdName)
		conn.Close()
	}

	// close main connection
	p.indiConn.Close()
	p.shouldExit = true
}
