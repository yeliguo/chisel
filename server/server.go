package chserver

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/jpillora/chisel/share"
	"github.com/jpillora/conncrypt"
	"golang.org/x/net/websocket"
)

type Server struct {
	*chshare.Logger
	key        string
	wsCount    int
	wsServer   websocket.Server
	httpServer *chshare.HTTPServer
	proxy      *httputil.ReverseProxy
}

func NewServer(key, proxy string) (*Server, error) {
	s := &Server{
		Logger:     chshare.NewLogger("server"),
		key:        key,
		wsServer:   websocket.Server{},
		httpServer: chshare.NewHTTPServer(),
	}
	s.wsServer.Handler = websocket.Handler(s.handleWS)

	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		if u.Host == "" {
			return nil, s.Errorf("Missing protocol (%s)", u)
		}
		s.proxy = httputil.NewSingleHostReverseProxy(u)
		//always use proxy host
		s.proxy.Director = func(r *http.Request) {
			r.URL.Scheme = u.Scheme
			r.URL.Host = u.Host
			r.Host = u.Host
		}
	}

	return s, nil
}

func (s *Server) Run(host, port string) error {
	if err := s.Start(host, port); err != nil {
		return err
	}
	return s.Wait()
}

func (s *Server) Start(host, port string) error {
	if s.key != "" {
		s.Infof("Authenication enabled")
	}
	if s.proxy != nil {
		s.Infof("Default proxy enabled")
	}
	s.Infof("Listening on %s...", port)

	return s.httpServer.GoListenAndServe(":"+port, http.HandlerFunc(s.handleHTTP))
}

func (s *Server) Wait() error {
	return s.httpServer.Wait()
}

func (s *Server) Close() error {
	//this should cause an error in the open websockets
	return s.httpServer.Close()
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	//websockets upgrade AND has chisel prefix
	if r.Header.Get("Upgrade") == "websocket" &&
		r.Header.Get("Sec-WebSocket-Protocol") == chshare.ProtocolVersion {
		s.wsServer.ServeHTTP(w, r)
		return
	}
	//proxy target was provided
	if s.proxy != nil {
		s.proxy.ServeHTTP(w, r)
		return
	}
	//missing :O
	w.WriteHeader(404)
}

func (s *Server) handleWS(ws *websocket.Conn) {

	conn := net.Conn(ws)

	if s.key != "" {
		conn = conncrypt.New(conn, &conncrypt.Config{Password: s.key})
	}

	configb := chshare.SizeRead(conn)
	config, err := chshare.DecodeConfig(configb)

	if err != nil {
		s.Infof("Handshake failed: %s", err)
		chshare.SizeWrite(conn, []byte("Handshake failed"))
		return
	}
	chshare.SizeWrite(conn, []byte("Handshake Success"))
	// s.Infof("success %+v\n", config)
	s.wsCount++

	newWebSocket(s, config, conn).handle()
}