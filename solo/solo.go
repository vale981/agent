package solo

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/clbanning/mxj"
	"github.com/fatih/color"

	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/proto/indihub"
)

var (
	getProperties    = []byte("<getProperties version='1.7'/>")
	getCCDProperties = "<getProperties device='%s' version='1.7'/>"
	enableBLOBNever  = "<enableBLOB device='%s'>Also</enableBLOB>"
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
}

func New(indiServerAddr string, tunnel INDIHubSoloTunnel) *Agent {
	return &Agent{
		indiServerAddr: indiServerAddr,
		tunnel:         tunnel,
		ccdConnMap:     make(map[string]net.Conn),
	}
}

func (p *Agent) Start(sessionID uint64, sessionToken string) error {
	p.sessionID = sessionID
	p.sessionToken = sessionToken

	// open connection to real INDI-server
	var err error
	p.indiConn, err = net.Dial("tcp", p.indiServerAddr)
	if err != nil {
		log.Printf("could not connect to INDI-server in solo-mode: %s\n", err)
		return err
	}
	defer p.indiConn.Close()

	// set connection to receive data
	if _, err := p.indiConn.Write(getProperties); err != nil {
		log.Printf("could not write to INDI-server in solo-mode: %s\n", err)
		return err
	}

	// listen INDI-server for data
	buf := make([]byte, lib.INDIServerMaxSendMsgSize, lib.INDIServerMaxSendMsgSize)
	xmlFlattener := lib.NewXmlFlattener()
	wg := sync.WaitGroup{}
	var connNum uint32
	for {
		if p.shouldExit {
			break
		}

		n, err := p.indiConn.Read(buf)
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
					go func(ccdName string, cNum uint32) {
						defer wg.Done()
						p.readFromCCD(ccdName, cNum)
					}(deviceStr, connNum)
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

func (p *Agent) readFromCCD(ccdName string, cNum uint32) {
	// open connection
	log.Println("Connecting to INDI-device:", ccdName)
	conn, err := net.Dial("tcp", p.indiServerAddr)
	if err != nil {
		log.Printf("could not connect to INDI-server for CCD '%s' in solo-mode: %s\n",
			ccdName, err)
		return
	}
	defer conn.Close()
	log.Println("...OK")

	p.setConnCCD(ccdName, conn)

	// set connection to receive data
	if _, err := conn.Write([]byte(fmt.Sprintf(getCCDProperties, ccdName))); err != nil {
		log.Printf("getProperties: could not write to INDI-server for %s in solo-mode: %s\n", ccdName, err)
		return
	}

	// enable BLOBS only
	if _, err := conn.Write([]byte(fmt.Sprintf(enableBLOBOnly, ccdName))); err != nil {
		log.Printf("getProperties: could not write to INDI-server for %s in solo-mode: %s\n", ccdName, err)
		return
	}

	// read data from INDI-server and send to tunnel
	buf := make([]byte, lib.INDIServerMaxSendMsgSize, lib.INDIServerMaxSendMsgSize)
	resp := &indihub.Response{
		Conn:         cNum,
		SessionID:    p.sessionID,
		SessionToken: p.sessionToken,
	}
	for {
		if p.shouldExit {
			break
		}

		n, err := conn.Read(buf)
		if err != nil {
			log.Printf("could not read from INDI-server for %s in solo-mode: %s\n", ccdName, err)
			break
		}

		// send data to tunnel
		resp.Data = buf[:n]
		if err := p.tunnel.Send(resp); err != nil {
			log.Printf("could not send to tunnel for %s in solo-mode: %s\n", ccdName, err)
			break
		}
	}
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
