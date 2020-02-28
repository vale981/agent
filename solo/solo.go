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

	setBLOBVector = []byte("<setBLOBVector")
	defTextVector = []byte("<defTextVector")
)

type INDIHubSoloTunnel interface {
	Send(response *indihub.Response) error
	CloseAndRecv() (*indihub.SoloSummary, error)
}

type Agent struct {
	indiServerAddr string
	indiConn       net.Conn
	tunnel         INDIHubSoloTunnel
	ccdDrivers     map[string]bool
	shouldExit     bool
}

func New(indiServerAddr string, tunnel INDIHubSoloTunnel, ccdDrivers []string) *Agent {
	ccdDriversMap := make(map[string]bool)
	for _, d := range ccdDrivers {
		ccdDriversMap[d] = true
	}

	return &Agent{
		indiServerAddr: indiServerAddr,
		tunnel:         tunnel,
		ccdDrivers:     ccdDriversMap,
	}
}

func (p *Agent) Start(indiServerAddr string, sessionID uint64, sessionToken string) error {
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

			// subscribe to BLOBs from CCDs
			if !bytes.HasPrefix(xmlCmd, defTextVector) {
				continue
			}

			mapVal, err := mxj.NewMapXml(xmlCmd, true)
			if err != nil {
				log.Println("could not parse XML chunk in solo-mode:", err)
				continue
			}
			defTextVectorMap, _ := mapVal.ValueForKey("defTextVector")
			if defTextVectorMap == nil {
				continue
			}
			defTextVectorVal := defTextVectorMap.(map[string]interface{})

			if nameStr, ok := defTextVectorVal["attr_name"].(string); ok && nameStr == "DRIVER_INFO" {
				if defTextVal, ok := defTextVectorVal["defText"].([]interface{}); ok {
					for _, driverInfo := range defTextVal {
						driverInfoVal := driverInfo.(map[string]interface{})
						if driverNameStr, ok := driverInfoVal["attr_name"].(string); ok && driverNameStr == "DRIVER_EXEC" {
							if execText, ok := driverInfoVal["#text"].(string); ok && p.ccdDrivers[execText] {
								if deviceStr, ok := defTextVectorVal["attr_device"].(string); ok {
									_, err := p.indiConn.Write([]byte(fmt.Sprintf(enableBLOB, deviceStr)))
									if err != nil {
										log.Printf("could not write to INDI-server in solo-mode: %s\n", err)
									}
									break
								}
							}
						}
					}
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
