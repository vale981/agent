package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"time"

	"github.com/fatih/color"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"

	"github.com/indihub-space/agent/broadcast"
	"github.com/indihub-space/agent/config"
	"github.com/indihub-space/agent/hostutils"
	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/logutil"
	"github.com/indihub-space/agent/manager"
	"github.com/indihub-space/agent/proto/indihub"
	"github.com/indihub-space/agent/proxy"
	"github.com/indihub-space/agent/solo"
	"github.com/indihub-space/agent/version"
	"github.com/indihub-space/agent/websockets"
)

const (
	defaultWSPort uint64 = 2020

	modeSolo      = "solo"
	modeBroadcast = "broadcast"
	modeShare     = "share"
	modeRobotic   = "robotic"
)

var (
	flagINDIServerManagerAddr   string
	flagPHD2ServerAddr          string
	flagINDIProfile             string
	flagToken                   string
	flagConfFile                string
	flagSoloINDIServerAddr      string
	flagBroadcastINDIServerAddr string
	flagCompress                bool
	flagWSServer                bool
	flagWSIsTLS                 bool
	flagWSPort                  uint64
	flagWSOrigins               string
	flagMode                    string

	indiServerAddr string

	httpClientSM = http.Client{}
)

func init() {
	flag.StringVar(
		&flagINDIServerManagerAddr,
		"indi-server-manager",
		"raspberrypi.local:8624",
		"INDI-server Manager address (host:port)",
	)
	flag.StringVar(
		&flagMode,
		"mode",
		modeSolo,
		`indihub-agent mode (deafult value is "solo"), there four modes:\n
solo - equipment sharing is not possible, you are connected to INDIHUB and contributing images
sharing - you are sharing equipment with another INDIHUB user (agent will output connection info)
broadcast - equipment sharing is not possible, you are broadcasting your experience to any number of INDIHUB users
robotic - equipment sharing is not possible, your equipment is controlled by INDIHUB AI (you can still watch what it is doing!) 
`,
	)
	flag.BoolVar(
		&flagCompress,
		"compress",
		true,
		"Enable gzip-compression",
	)
	flag.StringVar(
		&flagSoloINDIServerAddr,
		"solo-indi-server",
		"localhost:7624",
		"agent INDI-server address (host:port) for solo-mode",
	)
	flag.StringVar(
		&flagBroadcastINDIServerAddr,
		"broadcast-indi-server",
		"localhost:7624",
		"agent INDI-server address (host:port) for broadcast-mode",
	)
	flag.StringVar(
		&flagPHD2ServerAddr,
		"phd2-server",
		"",
		"PHD2-server address (host:port)",
	)
	flag.StringVar(
		&flagToken,
		"token",
		"",
		"token - can be requested at https://indihub.space/token",
	)
	flag.StringVar(
		&flagConfFile,
		"conf",
		"indihub.json",
		"INDIHub Agent config file path",
	)
	flag.StringVar(
		&flagINDIProfile,
		"indi-profile",
		"",
		"Name of INDI-profile to share via indihub",
	)
	flag.BoolVar(
		&flagWSServer,
		"ws-server",
		true,
		"launch Websocket server to control equipment via Websocket API",
	)
	flag.BoolVar(
		&flagWSIsTLS,
		"ws-tls",
		false,
		"serve web-socket over TLS with self-signed certificate",
	)
	flag.Uint64Var(
		&flagWSPort,
		"ws-port",
		defaultWSPort,
		"port to start web socket-server on",
	)
	flag.StringVar(
		&flagWSOrigins,
		"ws-origins",
		"",
		"comma-separated list of origins allowed to connect to WS-server",
	)
}

func main() {
	flag.Parse()

	if flagMode != modeSolo && flagMode != modeShare && flagMode != modeBroadcast && flagMode != modeRobotic {
		log.Fatalf("Unknown mode '%s' provided\n", flagMode)
	}

	indiHubAddr := "relay.indihub.io:7668" // tls one
	if logutil.IsDev {
		indiHubAddr = "localhost:7667" // TODO: change this to optional DEV server
	}

	if flagINDIServerManagerAddr == "" {
		log.Fatal("'indi-server-manager' parameter is missing, the 'host:port' format is expected")
	}

	indiHost, _, err := net.SplitHostPort(flagINDIServerManagerAddr)
	if err != nil {
		log.Fatal("Bad syntax for 'indi-server-manager' parameter, the 'host:port' format is expected")
	}

	if flagINDIProfile == "" {
		log.Fatal("'indi-profile' parameter is required")
	}

	// read token from flag or from config file if exists
	if flagToken == "" {
		conf, err := config.Read(flagConfFile)
		if err == nil {
			flagToken = conf.Token
		}
	}

	// connect to INDI-server Manager
	log.Printf("Connection to local INDI-Server Manager on %s...\n", flagINDIServerManagerAddr)
	managerClient := manager.NewClient(flagINDIServerManagerAddr)
	running, currINDIProfile, err := managerClient.GetStatus()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("...OK")

	// start required profile if it is not active and running
	if !running || currINDIProfile != flagINDIProfile {
		log.Printf("Setting active INDI-profile to '%s'\n", flagINDIProfile)
		if err := managerClient.StopServer(); err != nil {
			log.Fatal(err)
		}
		if err := managerClient.StartProfile(flagINDIProfile); err != nil {
			log.Fatal(err)
		}
	} else {
		log.Printf("INDI-server is running with active INDI-profile '%s'\n", flagINDIProfile)
	}

	// get profile connect data
	indiProfile, err := managerClient.GetProfile(flagINDIProfile)
	if err != nil {
		log.Fatalf("could not get INDI-profile from INDI-server manager: %s", err)
	}
	indiServerAddr = fmt.Sprintf("%s:%d", indiHost, indiProfile.Port)

	// get profile drivers data
	indiDrivers, err := managerClient.GetDrivers()
	if err != nil {
		log.Fatalf("could not get INDI-drivers info from INDI-server manager: %s", err)
	}
	log.Println("INDIDrivers:")
	for _, d := range indiDrivers {
		log.Printf("%+v", *d)
	}

	// test connect to local INDI-server
	log.Printf("Test connection to local INDI-Server on %s...\n", indiServerAddr)
	indiConn, err := net.Dial("tcp", indiServerAddr)
	if err != nil {
		log.Fatal(err)
	}
	indiConn.Close()
	log.Println("...OK")

	if flagPHD2ServerAddr != "" {
		log.Printf("Test connection to local PHD2-Server on %s...\n", flagPHD2ServerAddr)
		phd2Conn, err := net.Dial("tcp", flagPHD2ServerAddr)
		if err != nil {
			log.Fatal(err)
		}
		phd2Conn.Close()
		log.Println("...OK")
	}

	// prepare indihub-host data
	indiHubHost := &indihub.INDIHubHost{
		Token: flagToken,
		Profile: &indihub.INDIProfile{
			Id:          indiProfile.ID,
			Name:        indiProfile.Name,
			Port:        indiProfile.Port,
			Autostart:   indiProfile.AutoStart,
			Autoconnect: indiProfile.AutoConnect,
		},
		Drivers:      make([]*indihub.INDIDriver, len(indiDrivers)),
		SoloMode:     flagMode == modeSolo,
		IsPHD2:       flagPHD2ServerAddr != "",
		IsRobotic:    flagMode == modeRobotic,
		IsBroadcast:  flagMode == modeBroadcast,
		AgentVersion: version.AgentVersion,
		Os:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	}
	ccdDrivers := []string{}
	for i, driver := range indiDrivers {
		if driver.Family == "CCDs" {
			ccdDrivers = append(ccdDrivers, driver.Binary)
		}
		indiHubHost.Drivers[i] = &indihub.INDIDriver{
			Binary:  driver.Binary,
			Family:  driver.Family,
			Label:   driver.Label,
			Version: driver.Version,
			Role:    driver.Role,
			Custom:  driver.Custom,
			Name:    driver.Name,
		}
	}

	log.Println("Connecting to the indihub.space cloud...")
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(lib.GRPCMaxSendMsgSize)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(lib.GRPCMaxRecvMsgSize)),
	}
	if flagCompress {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")))
	}

	if logutil.IsDev {
		opts = append(opts, grpc.WithInsecure())
	} else {
		tlsConfig := &tls.Config{}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}

	conn, err := grpc.Dial(
		indiHubAddr,
		opts...,
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("...OK")

	indiHubClient := indihub.NewINDIHubClient(conn)

	// register host
	regInfo, err := indiHubClient.RegisterHost(context.Background(), indiHubHost)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Current agent version:", version.AgentVersion)
	log.Println("Latest agent version:", regInfo.AgentVersion)

	if version.AgentVersion < regInfo.AgentVersion {
		yc := color.New(color.FgYellow)
		yc.Println()
		yc.Println("                                ************************************************************")
		yc.Println("                                *          WARNING: you version of agent is outdated!      *")
		yc.Println("                                *                                                          *")
		yc.Println("                                *          Please download the latest version from:        *")
		yc.Println("                                *          https://indihub.space/downloads                 *")
		yc.Println("                                *                                                          *")
		yc.Println("                                ************************************************************")
		yc.Println("                                                                                            ")
	}

	log.Printf("Access token: %s\n", regInfo.Token)
	log.Printf("Host session token: %s\n", regInfo.SessionIDPublic)

	// create config for new host if flag wasn't provided
	if flagToken == "" {
		conf := &config.Config{
			Token: regInfo.Token,
		}
		if err := config.Write(flagConfFile, conf); err != nil {
			log.Printf("Could not create config file %s: %s", flagConfFile, err)
		}
	}

	// start WS-server
	wsServer := websockets.NewWsServer(
		regInfo.Token,
		indiServerAddr,
		flagPHD2ServerAddr,
		flagWSPort,
		flagWSIsTLS,
		flagWSOrigins,
	)
	go wsServer.Start()

	// start session
	switch flagMode {

	case modeSolo:
		// solo mode - equipment sharing is not available but host still sends all images to INDIHUB
		log.Println("'solo' parameter was provided. Your session is in solo-mode: equipment sharing is not available")
		log.Println("Starting INDIHUB agent in solo mode!")

		soloClient, err := indiHubClient.SoloMode(context.Background())
		if err != nil {
			log.Fatalf("Could not start agent in solo mode: %v", err)
		}

		soloAgent := solo.New(
			indiServerAddr,
			soloClient,
			ccdDrivers,
		)

		go func() {
			sigint := make(chan os.Signal, 1)
			signal.Notify(sigint, os.Interrupt, os.Kill)

			<-sigint

			// stop WS-server
			wsServer.Stop()

			log.Println("Closing INDIHUB solo-session")

			// close connections to local INDI-server and to INDI client
			soloAgent.Close()

			time.Sleep(1 * time.Second)

			// close grpc client connection
			conn.Close()
		}()

		// start solo mode INDI-server tcp-proxy
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			soloAgent.Start(regInfo.SessionID, regInfo.SessionIDPublic)
		}()

		wg.Wait()

	case modeShare, modeRobotic:
		// main equipment sharing mode
		if flagMode == modeRobotic {
			log.Println("'robotic' parameter was provided. Your session is in robotic-mode: equipment sharing is not available")
		}
		// open INDI server tunnel
		log.Println("Starting INDI-Server in the cloud...")
		indiServTunnel, err := indiHubClient.INDIServer(
			context.Background(),
		)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("...OK")

		indiFilterConf := &hostutils.INDIFilterConfig{} // TODO: add reading config
		indiFilter := hostutils.NewINDIFilter(indiFilterConf)
		indiServerProxy := proxy.New("INDI-Server", indiServerAddr, indiServTunnel, indiFilter)

		// start PHD2 server proxy if specified
		var phd2ServerProxy *proxy.TcpProxy
		if flagPHD2ServerAddr != "" {
			// open PHD2 server tunnel
			log.Println("Starting PHD2-Server in the cloud...")
			phd2ServTunnel, err := indiHubClient.PHD2Server(
				context.Background(),
			)
			if err != nil {
				log.Fatal(err)
			}
			log.Println("...OK")
			phd2ServerProxy = proxy.New("PHD2-Server", flagPHD2ServerAddr, phd2ServTunnel, nil)
		}

		go func() {
			sigint := make(chan os.Signal, 1)
			signal.Notify(sigint, os.Interrupt, os.Kill)

			<-sigint

			// stop WS-server
			wsServer.Stop()

			// close connections to tunnels
			indiServTunnel.CloseSend()
			if phd2ServerProxy != nil {
				phd2ServerProxy.Tunnel.CloseSend()
			}

			// close grpc client connection
			conn.Close()

			// close connections to local INDI-server and PHD2-Server
			indiServerProxy.Close()
			if phd2ServerProxy != nil {
				phd2ServerProxy.Close()
			}
		}()

		serverAddrChan := make(chan proxy.PublicServerAddr, 3)

		wg := sync.WaitGroup{}

		// INDI Server Proxy start
		waitNum := 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			indiServerProxy.Start(serverAddrChan, regInfo.SessionID, regInfo.SessionIDPublic)
		}()

		if flagPHD2ServerAddr != "" {
			waitNum = 2
			wg.Add(1)
			go func() {
				defer wg.Done()
				phd2ServerProxy.Start(serverAddrChan, regInfo.SessionID, regInfo.SessionIDPublic)
			}()
		}

		addrData := []proxy.PublicServerAddr{}
		for i := 0; i < waitNum; i++ {
			sAddr := <-serverAddrChan
			addrData = append(addrData, sAddr)
		}

		c := color.New(color.FgCyan)
		gc := color.New(color.FgGreen)
		yc := color.New(color.FgYellow)
		rc := color.New(color.FgMagenta)
		if flagMode != modeRobotic {
			c.Println()
			c.Println("                                ************************************************************")
			c.Println("                                *               INDIHUB public address list!!              *")
			c.Println("                                ************************************************************")
			c.Println("                                                                                            ")
			for _, sAddr := range addrData {
				gc.Printf("                                   %s: %s\n", sAddr.Name, sAddr.Addr)
			}
			c.Println("                                                                                            ")
			c.Println("                                ************************************************************")
			c.Println()
			c.Println("                                Please provide your guest with this information:")
			c.Println()
			c.Println("                                1. Public address list from the above")
			c.Println("                                2. Focal length and aperture of your main telescope")
			c.Println("                                3. Focal length and aperture of your guiding telescope")
			c.Println("                                4. Type of guiding you use: PHD2 or guiding via camera")
			c.Println("                                5. Names of your imaging camera and guiding cameras")
			c.Println()
			yc.Println("                                NOTE: These public addresses will be available ONLY until")
			yc.Println("                                agent is running! (Ctrl+C will stop the session)")
			c.Println()
		} else {
			c.Println()
			c.Println("                                ************************************************************")
			c.Println("                                *               INDIHUB robotic-session started!!          *")
			c.Println("                                ************************************************************")
			c.Println("                                                                                            ")
		}

		wg.Wait()

		c.Println()
		c.Println("                                ************************************************************")
		c.Println("                                *               INDIHUB session finished!!                 *")
		c.Println("                                ************************************************************")
		c.Println("                                                                                            ")
		if flagMode != modeRobotic {
			for _, sAddr := range addrData {
				rc.Printf("                                   %s: %s - CLOSED!!\n", sAddr.Name, sAddr.Addr)
			}
		} else {
			c.Println("                                *         INDIHUB robotic-session finished.                 *")
			c.Println("                                *         Thank you for your contribution!                  *")
		}
		c.Println("                                                                                            ")
		c.Println("                                ************************************************************")

	case modeBroadcast:
		// broadcast - broadcasting all replies from INDI-server to INDIHUB, equipment sharing is not available
		log.Println("Starting INDIHUB agent in broadcast mode!")

		broadcastClient, err := indiHubClient.BroadcastINDIServer(context.Background())
		if err != nil {
			log.Fatalf("Could not start agent in broadcast mode: %v", err)
		}

		broadcastProxy := broadcast.New(
			"INDI-Server Solo-mode",
			indiServerAddr,
			broadcastClient,
		)

		go func() {
			sigint := make(chan os.Signal, 1)
			signal.Notify(sigint, os.Interrupt, os.Kill)

			<-sigint

			// stop WS-server
			wsServer.Stop()

			log.Println("Closing INDIHUB solo-session")

			// close connections to local INDI-server and to INDI client
			broadcastProxy.Close()

			time.Sleep(1 * time.Second)

			// close grpc client connection
			conn.Close()
		}()

		// start broadcast mode INDI-server tcp-proxy
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			broadcastProxy.Start(regInfo.SessionID, regInfo.SessionIDPublic, flagBroadcastINDIServerAddr)
		}()

		wg.Wait()
	}
}
