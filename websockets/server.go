package websockets

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

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

type WsServer struct {
	token          string
	indiServerAddr string
	phd2ServerAddr string
	wsPort         uint64
	isTLS          bool
	origins        string

	e        *echo.Echo
	upgrader websocket.Upgrader
	connList []net.Conn
}

func NewWsServer(token string, indiServerAddr string, phd2ServerAddr string, wsPort uint64, isTLS bool, origins string) *WsServer {
	wsServer := &WsServer{
		token:          token,
		indiServerAddr: indiServerAddr,
		phd2ServerAddr: phd2ServerAddr,
		wsPort:         wsPort,
		isTLS:          isTLS,
		e:              echo.New(),
		upgrader:       websocket.Upgrader{},
		connList:       []net.Conn{},
	}

	if logutil.IsDev {
		allowedOrigins["localhost"] = true
	}

	// add optional additional origins
	for _, orig := range strings.Split(origins, ",") {
		allowedOrigins[strings.TrimSpace(orig)] = true
	}

	// allow WS connections only from number of domains
	wsServer.upgrader.CheckOrigin = func(r *http.Request) bool {
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
		return allowedOrigins[host]
	}

	return wsServer
}

func (s *WsServer) newIndiConnection(c echo.Context) error {
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

func (s *WsServer) newPHD2Connection(c echo.Context) error {
	return nil
}

func (s *WsServer) Start() {
	s.e.HideBanner = true
	s.e.HidePort = true

	if !logutil.IsDev {
		s.e.Logger.SetLevel(elog.OFF)
	}

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
	// localhost for dev-mode
	if logutil.IsDev {
		allowOrigins = append(allowOrigins, "http://localhost:5000")
	}
	s.e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: allowOrigins,
		AllowMethods: []string{http.MethodGet},
	}))

	// set auth middleware
	s.e.Use(middleware.KeyAuthWithConfig(middleware.KeyAuthConfig{
		KeyLookup: "query:token",
		Validator: func(token string, eCtx echo.Context) (b bool, err error) {
			return token == s.token, nil
		},
	}))

	s.e.GET("/indiserver", s.newIndiConnection)
	s.e.GET("/phd2server", s.newPHD2Connection)

	// check if we are running TLS
	if s.isTLS {
		// generate self-signed cert to serve WS over TLS
		keyFile, certFile, err := getSelfSignedCert()
		if err != nil {
			log.Println("could not start WSS server, cert generation failed:", err)
			return
		}
		err = s.e.StartTLS(fmt.Sprintf(":%d", s.wsPort), certFile, keyFile)
		if err != nil {
			log.Println("WSS server error:", err)
		}
		return
	}

	// run WS
	err := s.e.Start(fmt.Sprintf(":%d", s.wsPort))
	if err != nil {
		log.Println("WSS server error:", err)
	}
}

func (s *WsServer) Stop() {
	for _, conn := range s.connList {
		conn.Close()
	}
	s.e.Shutdown(context.Background())
}
