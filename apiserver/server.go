package apiserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/indihub-space/agent/version"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	elog "github.com/labstack/gommon/log"

	"github.com/indihub-space/agent/lib"
	"github.com/indihub-space/agent/logutil"
)

var allowedOrigins = map[string]bool{
	"indihub.space":      true,
	"app.indihub.space":  true,
	"kids.indihub.space": true,
}

// AgentMode provides interface to operate with agent from API-server
type AgentMode interface {
	Start()
	Stop()
	GetStatus() map[string]interface{}
}

type APIServer struct {
	token          string
	indiServerAddr string
	phd2ServerAddr string
	port           uint64
	isTLS          bool
	origins        string

	e        *echo.Echo
	upgrader websocket.Upgrader
	connList []net.Conn

	indiProfile string
	currMode    string
	agentModes  map[string]AgentMode
}

func NewAPIServer(token string, indiServerAddr string, phd2ServerAddr string, port uint64, isTLS bool, origins string,
	currMode string, indiProfile string, agentModes map[string]AgentMode) *APIServer {

	apiServer := &APIServer{
		token:          token,
		indiServerAddr: indiServerAddr,
		phd2ServerAddr: phd2ServerAddr,
		port:           port,
		isTLS:          isTLS,
		e:              echo.New(),
		upgrader: websocket.Upgrader{
			EnableCompression: true,
		},
		connList:    []net.Conn{},
		indiProfile: indiProfile,
		currMode:    currMode,
		agentModes:  agentModes,
	}

	if logutil.IsDev {
		allowedOrigins["localhost"] = true
	}

	// add optional additional origins
	for _, orig := range strings.Split(origins, ",") {
		allowedOrigins[strings.TrimSpace(orig)] = true
	}

	// allow WS connections only from number of domains
	apiServer.upgrader.CheckOrigin = func(r *http.Request) bool {
		origin := r.Header["Origin"]
		if len(origin) == 0 {
			return false
		}
		u, err := url.Parse(origin[0])
		if err != nil {
			return false
		}
		host, _, err := net.SplitHostPort(u.Host)
		if err != nil {
			return false
		}

		// check both host and host:port from --api-origins param values
		return allowedOrigins[host] || allowedOrigins[u.Host]
	}

	return apiServer
}

func (s *APIServer) newIndiConnection(c echo.Context) error {
	// upgrade to WS connection
	ws, err := s.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	// open connection to INDI-Server
	conn, err := net.Dial("tcp", s.indiServerAddr)
	if err != nil {
		return err
	}

	// add to connection list
	s.connList = append(s.connList, conn)

	// read messages from INDI-server and write them to WS
	go func(indiConn net.Conn, wsConn *websocket.Conn) {
		buf := make([]byte, lib.INDIServerMaxSendMsgSize, lib.INDIServerMaxSendMsgSize)
		xmlFlattener := lib.NewXmlFlattener()
		for {
			// read from INDI-server
			n, err := indiConn.Read(buf)
			if err != nil {
				indiConn.Close()
				return
			}

			jsonMessages := xmlFlattener.ConvertChunkToJSON(buf[:n])

			// Write to WS
			for _, m := range jsonMessages {
				err = wsConn.WriteMessage(websocket.TextMessage, m)
				if err != nil {
					indiConn.Close()
					return
				}
			}
		}
	}(conn, ws)

	// read messages from WS and write them to INDI-server
	xmlFlattener := lib.NewXmlFlattener()
	for {
		// Read from WS
		_, msg, err := ws.ReadMessage()
		if err != nil {
			conn.Close()
			return err
		}

		xmlMsg, err := xmlFlattener.ConvertJSONToXML(msg)
		if err != nil {
			log.Printf("could not convert json '%s' to xml: %s", string(msg), err)
			continue
		}

		// write to INDI server
		_, err = conn.Write(xmlMsg)
		if err != nil {
			conn.Close()
			return err
		}
	}
}

func (s *APIServer) newPHD2Connection(c echo.Context) error {
	return nil
}

func (s *APIServer) getRestart(c echo.Context) error {
	// restart current mode
	if curAgentMode, ok := s.agentModes[s.currMode]; ok {
		curAgentMode.Stop()
		time.Sleep(1 * time.Second)
		curAgentMode.Start()
	}

	c.JSONPretty(http.StatusOK, s.agentStatus(), "    ")

	return nil
}

func (s *APIServer) getStatus(c echo.Context) error {
	c.JSONPretty(http.StatusOK, s.agentStatus(), "    ")

	return nil
}

func (s *APIServer) changeMode(c echo.Context) error {
	newMode := c.Param("new_mode")

	// don't do anything if agent is laready in this mode
	if newMode == s.currMode {
		c.JSONPretty(http.StatusOK, s.agentStatus(), "    ")
		return nil
	}

	// get agent new mode
	newAgentMode, ok := s.agentModes[newMode]
	if !ok {
		c.JSON(
			http.StatusBadRequest,
			map[string]interface{}{
				"message": "unknown indihub-agent mode: " + newMode,
			},
		)
		return nil
	}

	// stop current mode
	if curAgentMode, ok := s.agentModes[s.currMode]; ok {
		curAgentMode.Stop()
	}

	time.Sleep(1 * time.Second)

	// start agent in new mode
	s.currMode = newMode
	newAgentMode.Start()

	c.JSONPretty(http.StatusOK, s.agentStatus(), "    ")

	return nil
}

func (s *APIServer) agentStatus() map[string]interface{} {
	agentStatus := map[string]interface{}{
		"version":     version.AgentVersion,
		"mode":        s.currMode,
		"indiProfile": s.indiProfile,
		"indiServer":  s.indiServerAddr,
		"phd2Server":  s.phd2ServerAddr,
	}

	supportedModes := make([]string, 0, len(s.agentModes))
	for key := range s.agentModes {
		supportedModes = append(supportedModes, key)
	}
	agentStatus["supportedModes"] = supportedModes

	if agentMode, ok := s.agentModes[s.currMode]; ok {
		for key, val := range agentMode.GetStatus() {
			agentStatus[key] = val
		}
	}

	return agentStatus
}

func (s *APIServer) Start() {
	s.e.HideBanner = true
	s.e.HidePort = true

	if !logutil.IsDev {
		s.e.Logger.SetLevel(elog.OFF)
	}

	// setup middle-wares

	s.e.Use(middleware.Recover())

	// set CORS in case browser decides to do pre-flight OPTIONS request
	// default ones
	allowOrigins := []string{}
	for orig := range allowedOrigins {
		allowOrigins = append(allowOrigins, "http://"+orig)
		allowOrigins = append(allowOrigins, "https://"+orig)
	}
	// optional ones
	for _, orig := range strings.Split(s.origins, ",") {
		allowOrigins = append(allowOrigins, "http://"+strings.TrimSpace(orig))
		allowOrigins = append(allowOrigins, "https://"+strings.TrimSpace(orig))
	}
	// add localhost for dev-mode
	if logutil.IsDev {
		allowOrigins = append(allowOrigins, "http://localhost:5000")
	}
	s.e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: allowOrigins,
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
		},
	}))

	// setup routing for WS and RESTful APIs

	// protected WS-API
	wsGroup := s.e.Group(
		"/websocket",
		// set auth middleware
		middleware.KeyAuthWithConfig(middleware.KeyAuthConfig{
			KeyLookup: "query:token",
			Validator: func(token string, eCtx echo.Context) (b bool, err error) {
				return token == s.token, nil
			},
		}),
	)
	wsGroup.GET("/indiserver", s.newIndiConnection)
	wsGroup.GET("/phd2server", s.newPHD2Connection)

	// protected RESTful API
	s.e.POST(
		"/mode/:new_mode",
		s.changeMode,
		// set auth middleware
		middleware.KeyAuthWithConfig(middleware.KeyAuthConfig{
			KeyLookup: "header:Authorization",
			Validator: func(token string, eCtx echo.Context) (b bool, err error) {
				return token == s.token, nil
			},
		}),
	)

	// public RESTful API
	s.e.GET("/status", s.getStatus)
	s.e.GET("/restart", s.getRestart)

	// start agent in a required mode
	agentMode, ok := s.agentModes[s.currMode]
	if !ok {
		log.Println("unknown agent mode:", s.currMode)
		return
	}
	agentMode.Start()

	// check if we are running TLS
	if s.isTLS {
		// generate self-signed cert to serve WS over TLS
		keyFile, certFile, err := getSelfSignedCert()
		if err != nil {
			log.Println("could not start API-server, self-signed certificate generating failed:", err)
			return
		}
		// start HTTP/WS API server over TLS
		err = s.e.StartTLS(fmt.Sprintf(":%d", s.port), certFile, keyFile)
		if err != nil && err != http.ErrServerClosed {
			log.Println("API-server error:", err)
		} else {
			log.Println("API-server was shutdown gracefully")
		}
		return
	}

	// start HTTP/WS API server
	err := s.e.Start(fmt.Sprintf(":%d", s.port))
	if err != nil && err != http.ErrServerClosed {
		log.Println("API-server error:", err)
	} else {
		log.Println("API-server was shutdown gracefully")
	}
}

func (s *APIServer) Stop() {
	if agentMode, ok := s.agentModes[s.currMode]; ok {
		agentMode.Stop()
	}
	for _, conn := range s.connList {
		conn.Close()
	}
	s.e.Shutdown(context.Background())
}
