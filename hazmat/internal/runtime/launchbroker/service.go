package launchbroker

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
)

type ServiceConfig struct {
	SocketPath      string
	SocketMode      os.FileMode
	ExpectedPeerUID int
	MaxRequestBytes int64
	ResolvePeerUID  PeerUIDResolver
	HandleLaunch    LaunchHandler
	Helper          HelperExecutorConfig
}

type Service struct {
	server *Server
	cancel context.CancelFunc
	ready  chan struct{}
	done   chan error

	closeOnce sync.Once
	closeErr  error
}

func StartService(ctx context.Context, cfg ServiceConfig) (*Service, error) {
	handler := cfg.HandleLaunch
	if handler == nil {
		var err error
		handler, err = NewHelperLaunchHandler(cfg.Helper)
		if err != nil {
			return nil, err
		}
	}
	server, err := Listen(ServerConfig{
		SocketPath:      cfg.SocketPath,
		SocketMode:      cfg.SocketMode,
		ExpectedPeerUID: cfg.ExpectedPeerUID,
		MaxRequestBytes: cfg.MaxRequestBytes,
		ResolvePeerUID:  cfg.ResolvePeerUID,
		HandleLaunch:    handler,
	})
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	serveCtx, cancel := context.WithCancel(ctx)
	service := &Service{
		server: server,
		cancel: cancel,
		ready:  make(chan struct{}),
		done:   make(chan error, 1),
	}
	go service.serve(serveCtx)
	return service, nil
}

func (s *Service) serve(ctx context.Context) {
	close(s.ready)
	err := s.server.Serve(ctx)
	closeErr := normalizeServiceCloseError(s.server.Close())
	s.done <- errors.Join(err, closeErr)
	close(s.done)
}

func (s *Service) SocketPath() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.SocketPath()
}

func (s *Service) Ready() <-chan struct{} {
	if s == nil {
		ready := make(chan struct{})
		close(ready)
		return ready
	}
	return s.ready
}

func (s *Service) Done() <-chan error {
	if s == nil {
		done := make(chan error, 1)
		done <- nil
		return done
	}
	return s.done
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.cancel()
		closeErr := normalizeServiceCloseError(s.server.Close())
		s.closeErr = errors.Join(closeErr, <-s.done)
	})
	return s.closeErr
}

func normalizeServiceCloseError(err error) error {
	if err == nil || errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
