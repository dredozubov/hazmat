package launchbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultSocketMode       os.FileMode = 0o600
	defaultMaxRequestBytes              = int64(1 << 20)
	defaultMaxResponseBytes             = int64(1 << 20)
	acceptPollInterval                  = 50 * time.Millisecond
)

type PeerUIDResolver func(*net.UnixConn) (int, error)

type LaunchHandler func(context.Context, ChildPlan) (LaunchResponse, error)

type LaunchResponse struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	ExitCode     int    `json:"exit_code,omitempty"`
	MetadataJSON string `json:"metadata_json,omitempty"`
}

type ServerConfig struct {
	SocketPath      string
	SocketMode      os.FileMode
	ExpectedPeerUID int
	MaxRequestBytes int64
	ResolvePeerUID  PeerUIDResolver
	HandleLaunch    LaunchHandler
}

type Server struct {
	cfg      serverConfig
	listener *net.UnixListener
}

type serverConfig struct {
	socketPath      string
	expectedPeerUID int
	maxRequestBytes int64
	resolvePeerUID  PeerUIDResolver
	handleLaunch    LaunchHandler
}

func Listen(cfg ServerConfig) (*Server, error) {
	normalized, mode, err := normalizeServerConfig(cfg)
	if err != nil {
		return nil, err
	}

	addr := net.UnixAddr{Name: normalized.socketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", &addr)
	if err != nil {
		return nil, fmt.Errorf("listen on launch broker socket %q: %w", normalized.socketPath, err)
	}
	if err := os.Chmod(normalized.socketPath, mode); err != nil {
		_ = listener.Close()
		_ = os.Remove(normalized.socketPath)
		return nil, fmt.Errorf("set launch broker socket mode: %w", err)
	}

	return &Server{
		cfg:      normalized,
		listener: listener,
	}, nil
}

func normalizeServerConfig(cfg ServerConfig) (serverConfig, os.FileMode, error) {
	if cfg.SocketPath == "" {
		return serverConfig{}, 0, errors.New("socket path is required")
	}
	socketPath := filepath.Clean(cfg.SocketPath)
	if !filepath.IsAbs(socketPath) || socketPath != cfg.SocketPath {
		return serverConfig{}, 0, fmt.Errorf("socket path %q must be absolute and clean", cfg.SocketPath)
	}
	if cfg.ExpectedPeerUID <= 0 {
		return serverConfig{}, 0, fmt.Errorf("expected peer uid must be positive, got %d", cfg.ExpectedPeerUID)
	}
	if cfg.HandleLaunch == nil {
		return serverConfig{}, 0, errors.New("launch handler is required")
	}

	mode := cfg.SocketMode
	if mode == 0 {
		mode = defaultSocketMode
	}
	resolver := cfg.ResolvePeerUID
	if resolver == nil {
		resolver = DefaultPeerUID
	}
	maxRequestBytes := cfg.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = defaultMaxRequestBytes
	}

	return serverConfig{
		socketPath:      socketPath,
		expectedPeerUID: cfg.ExpectedPeerUID,
		maxRequestBytes: maxRequestBytes,
		resolvePeerUID:  resolver,
		handleLaunch:    cfg.HandleLaunch,
	}, mode, nil
}

func (s *Server) SocketPath() string {
	if s == nil {
		return ""
	}
	return s.cfg.socketPath
}

func (s *Server) Close() error {
	if s == nil || s.listener == nil {
		return nil
	}
	listenErr := s.listener.Close()
	removeErr := os.Remove(s.cfg.socketPath)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return errors.Join(listenErr, removeErr)
	}
	return listenErr
}

func (s *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := s.ServeOnce(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

func (s *Server) ServeOnce(ctx context.Context) error {
	if s == nil || s.listener == nil {
		return errors.New("launch broker server is not listening")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		if err := s.listener.SetDeadline(time.Now().Add(acceptPollInterval)); err != nil {
			return fmt.Errorf("set launch broker accept deadline: %w", err)
		}
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			if isTimeout(err) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					continue
				}
			}
			return fmt.Errorf("accept launch broker connection: %w", err)
		}
		return s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn *net.UnixConn) error {
	defer conn.Close()

	uid, err := s.cfg.resolvePeerUID(conn)
	if err != nil {
		return writeResponse(conn, LaunchResponse{OK: false, Error: fmt.Sprintf("authenticate peer: %v", err)})
	}
	peer, err := AuthenticatePeer(uid, s.cfg.expectedPeerUID)
	if err != nil {
		return writeResponse(conn, LaunchResponse{OK: false, Error: err.Error()})
	}

	var req LaunchRequest
	dec := json.NewDecoder(io.LimitReader(conn, s.cfg.maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return writeResponse(conn, LaunchResponse{OK: false, Error: fmt.Sprintf("decode launch request: %v", err)})
	}

	verified, err := VerifyLaunchRequest(peer, req)
	if err != nil {
		return writeResponse(conn, LaunchResponse{OK: false, Error: err.Error()})
	}
	plan, err := NewChildPlan(verified, ChildFDPolicyCloseInherited)
	if err != nil {
		return writeResponse(conn, LaunchResponse{OK: false, Error: err.Error()})
	}

	resp, err := s.cfg.handleLaunch(ctx, plan)
	if err != nil {
		return writeResponse(conn, LaunchResponse{OK: false, Error: err.Error()})
	}
	if !resp.OK && resp.Error == "" {
		resp.Error = "launch handler returned an unsuccessful response"
	}
	return writeResponse(conn, resp)
}

type Client struct {
	SocketPath       string
	Timeout          time.Duration
	MaxResponseBytes int64
}

func (c Client) Launch(ctx context.Context, req LaunchRequest) (LaunchResponse, error) {
	socketPath := filepath.Clean(c.SocketPath)
	if c.SocketPath == "" || !filepath.IsAbs(socketPath) || socketPath != c.SocketPath {
		return LaunchResponse{}, fmt.Errorf("socket path %q must be absolute and clean", c.SocketPath)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return LaunchResponse{}, fmt.Errorf("connect launch broker: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return LaunchResponse{}, fmt.Errorf("set launch broker deadline: %w", err)
		}
	}
	maxResponseBytes := c.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		if resp, readErr := readResponse(conn, maxResponseBytes); readErr == nil {
			return responseResult(resp)
		}
		return LaunchResponse{}, fmt.Errorf("encode launch request: %w", err)
	}

	resp, err := readResponse(conn, maxResponseBytes)
	if err != nil {
		return LaunchResponse{}, fmt.Errorf("decode launch response: %w", err)
	}
	return responseResult(resp)
}

func responseResult(resp LaunchResponse) (LaunchResponse, error) {
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "launch broker rejected request"
		}
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func writeResponse(conn *net.UnixConn, resp LaunchResponse) error {
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		return fmt.Errorf("write launch broker response: %w", err)
	}
	return nil
}

func readResponse(r io.Reader, maxBytes int64) (LaunchResponse, error) {
	var resp LaunchResponse
	dec := json.NewDecoder(io.LimitReader(r, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		return LaunchResponse{}, err
	}
	return resp, nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
