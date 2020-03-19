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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"

	"github.com/indihub-space/agent/apiserver"
	"github.com/indihub-space/agent/config"
	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/logutil"
	"github.com/indihub-space/agent/manager"
	"github.com/indihub-space/agent/proto/indihub"
	"github.com/indihub-space/agent/share"
	"github.com/indihub-space/agent/solo"
	"github.com/indihub-space/agent/version"
)

const (
	defaultAPIPort uint64 = 2020
)

var (
	flagINDIServerManagerAddr string
	flagPHD2ServerAddr        string
	flagINDIServerAddr        string
	flagINDIProfile           string
	flagToken                 string
	flagConfFile              string
	flagCompress              bool
	flagAPITLS                bool
	flagAPIPort               uint64
	flagAPIOrigins            string
	flagMode                  string

	indiServerAddr string

	httpClientSM = http.Client{}
)

func init() {
	// restrict  number of system-threads to number of cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.StringVar(
		&flagINDIServerManagerAddr,
		"indi-server-manager",
		"raspberrypi.local:8624",
		"INDI-server Manager address (host:port)",
	)
	flag.StringVar(
		&flagINDIServerAddr,
		"indi-server",
		"",
		"INDI-server address (host:port) to connect without Web Manager",
	)
	flag.StringVar(
		&flagMode,
		"mode",
		lib.ModeSolo,
		`indihub-agent mode (deafult value is "solo"), there four modes:\n
solo - equipment sharing is not possible, you are connected to INDIHUB and contributing images
share - you are sharing equipment with another INDIHUB user (agent will output connection info)
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
		&flagAPITLS,
		"api-tls",
		false,
		"serve API-server over TLS with self-signed certificate",
	)
	flag.Uint64Var(
		&flagAPIPort,
		"api-port",
		defaultAPIPort,
		"port to start API-server on",
	)
	flag.StringVar(
		&flagAPIOrigins,
		"api-origins",
		"",
		"comma-separated list of origins allowed to connect to API-server",
	)
}

func main() {
	flag.Parse()

	if flagMode != lib.ModeSolo && flagMode != lib.ModeShare && flagMode != lib.ModeRobotic {
		log.Fatalf("Unknown mode '%s' provided\n", flagMode)
	}

	indiHubAddr := "relay.indihub.io:7668" // tls one
	if logutil.IsDev {
		indiHubAddr = "localhost:7667" // TODO: change this to optional DEV server
	}

	indiServerAddr := ""
	indiDrivers := []*lib.INDIDriver{}
	indiProfile := &lib.INDIProfile{}
	if flagINDIServerAddr != "" {
		// connect to INDI-server directly without Web Manager
		if _, _, err := net.SplitHostPort(flagINDIServerAddr); err != nil {
			log.Fatal("Bad syntax for 'indi-server' parameter, the 'host:port' format is expected")
		}
		log.Println("Will try to connect directly to INDI-server (Web Manager is not used)")
		indiProfile.Name = flagINDIServerAddr // to let backend know that no Web Manager was used
		indiServerAddr = flagINDIServerAddr
	} else {
		// connect to INDI-server using info from Web Manager
		indiHost, _, err := net.SplitHostPort(flagINDIServerManagerAddr)
		if err != nil {
			log.Fatal("Bad syntax for 'indi-server-manager' parameter, the 'host:port' format is expected")
		}
		if flagINDIProfile == "" {
			log.Fatal("'indi-profile' parameter is required")
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
		indiProfile, err = managerClient.GetProfile(flagINDIProfile)
		if err != nil {
			log.Fatalf("could not get INDI-profile from INDI-server manager: %s", err)
		}
		indiServerAddr = fmt.Sprintf("%s:%d", indiHost, indiProfile.Port)

		// get profile drivers data
		indiDrivers, err = managerClient.GetDrivers()
		if err != nil {
			log.Fatalf("could not get INDI-drivers info from INDI-server manager: %s", err)
		}
		log.Println("INDIDrivers:")
		for _, d := range indiDrivers {
			log.Printf("%+v", *d)
		}
	}

	// read token from flag or from config file if exists
	if flagToken == "" {
		conf, err := config.Read(flagConfFile)
		if err == nil {
			flagToken = conf.Token
		}
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
		SoloMode:     flagMode == lib.ModeSolo,
		IsPHD2:       flagPHD2ServerAddr != "",
		IsRobotic:    flagMode == lib.ModeRobotic,
		IsBroadcast:  false,
		AgentVersion: version.AgentVersion,
		Os:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	}
	for i, driver := range indiDrivers {
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
	// close grpc client connection at the very end
	defer conn.Close()

	indiHubClient := indihub.NewINDIHubClient(conn)

	// register host
	regInfo, err := indiHubClient.RegisterHost(context.Background(), indiHubHost)
	if err != nil {
		log.Fatal(err)
	}

	version.CheckAgentVersion(regInfo.AgentVersion)

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

	// prepare all modes
	soloMode := solo.NewMode(indiHubClient, regInfo, indiServerAddr)
	shareMode := share.NewMode(indiHubClient, regInfo, indiServerAddr, flagPHD2ServerAddr, lib.ModeShare)
	roboticMode := share.NewMode(indiHubClient, regInfo, indiServerAddr, flagPHD2ServerAddr, lib.ModeRobotic)

	// start API-server
	apiServer := apiserver.NewAPIServer(
		regInfo.Token,
		indiServerAddr,
		flagPHD2ServerAddr,
		flagAPIPort,
		flagAPITLS,
		flagAPIOrigins,
		flagMode,
		flagINDIProfile,
		map[string]apiserver.AgentMode{
			lib.ModeSolo:    soloMode,
			lib.ModeShare:   shareMode,
			lib.ModeRobotic: roboticMode,
		},
	)

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)

		sig := <-sigint
		log.Println("Stopping API-server gracefully. OS signal received:", sig)

		// close connections to local INDI-server
		apiServer.Stop()
	}()

	// start API-server and indihub-agent in the current mode
	apiServer.Start()
}
