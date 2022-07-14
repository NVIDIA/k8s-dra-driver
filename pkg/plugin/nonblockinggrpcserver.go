package plugin

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"google.golang.org/grpc"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha1"
)

// NonBlocking server
type nonBlockingGRPCServer struct {
	wg      sync.WaitGroup
	server  *grpc.Server
	cleanup func()
}

func (s *nonBlockingGRPCServer) Start(endpoint string, ns drapbv1.NodeServer) {

	s.wg.Add(1)

	go s.serve(endpoint, ns)
}

func (s *nonBlockingGRPCServer) Wait() {
	s.wg.Wait()
}

func (s *nonBlockingGRPCServer) Stop() {
	s.server.GracefulStop()
	s.cleanup()
}

func (s *nonBlockingGRPCServer) ForceStop() {
	s.server.Stop()
	s.cleanup()
}

func (s *nonBlockingGRPCServer) serve(ep string, ns drapbv1.NodeServer) {
	listener, cleanup, err := listen(ep)
	if err != nil {
		fmt.Printf("Failed to listen: %v", err)
	}

	opts := []grpc.ServerOption{}
	server := grpc.NewServer(opts...)
	s.server = server
	s.cleanup = cleanup

	if ns != nil {
		drapbv1.RegisterNodeServer(server, ns)
	}

	server.Serve(listener)

}

func parse(ep string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			return s[0], s[1], nil
		}
		return "", "", fmt.Errorf("invalid endpoint: %v", ep)
	}
	// Assume everything else is a file path for a Unix Domain Socket.
	return "unix", ep, nil
}

func listen(endpoint string) (net.Listener, func(), error) {
	proto, addr, err := parse(endpoint)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {}
	if proto == "unix" {
		addr = "/" + addr
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) { //nolint: vetshadow
			return nil, nil, fmt.Errorf("%s: %q", addr, err)
		}
		cleanup = func() {
			os.Remove(addr)
		}
	}

	l, err := net.Listen(proto, addr)
	return l, cleanup, err
}

func NewNonBlockingGRPCServer() *nonBlockingGRPCServer {
	return &nonBlockingGRPCServer{}
}
