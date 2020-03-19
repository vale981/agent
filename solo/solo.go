package solo

import (
	"bytes"
	"fmt"
	"log"
	"net"

	"github.com/clbanning/mxj"

	"github.com/fatih/color"

	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/proto/indihub"
)

var (
	getProperties = []byte("<getProperties version='1.7'/>")
	enableBLOB    = "<enableBLOB device='%s'>Also</enableBLOB>"

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
}

func New(indiServerAddr string, tunnel INDIHubSoloTunnel) *Agent {
	return &Agent{
		indiServerAddr: indiServerAddr,
		tunnel:         tunnel,
	}
}

func (p *Agent) Start(sessionID uint64, sessionToken string) error {
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
	subscribedCCDs := map[string]bool{}
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
			// catch images
			if bytes.HasPrefix(xmlCmd, setBLOBVector) {
				err := p.sendImages(xmlCmd, 1, sessionID, sessionToken)
				if err != nil {
					log.Println("could not send image to INDIHUB in solo-mode:", err)
				}
				continue
			}

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
				if deviceStr, ok := defNumberVectorVal["attr_device"].(string); ok && !subscribedCCDs[deviceStr] {
					_, err := p.indiConn.Write([]byte(fmt.Sprintf(enableBLOB, deviceStr)))
					if err != nil {
						log.Printf("could not write to INDI-server in solo-mode: %s\n", err)
					}
					subscribedCCDs[deviceStr] = true
					break
				}
			}
		}
	}

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

func (p *Agent) Close() {
	p.indiConn.Close()
	p.shouldExit = true
}

func (p *Agent) sendImages(imagesData []byte, cNum uint32, sessID uint64, sessToken string) error {
	resp := &indihub.Response{
		Data:         imagesData,
		Conn:         cNum,
		SessionID:    sessID,
		SessionToken: sessToken,
	}

	return p.tunnel.Send(resp)
}
