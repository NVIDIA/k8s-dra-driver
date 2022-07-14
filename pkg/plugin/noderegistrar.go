package plugin

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

type nodeRegistrarConfig struct {
	draDriverName          string
	draAddress             string
	pluginRegistrationPath string
}
type nodeRegistrar struct {
	config nodeRegistrarConfig
}

// NewRegistrar returns an initialized nodeRegistrar instance
func newRegistrar(config nodeRegistrarConfig) *nodeRegistrar {
	return &nodeRegistrar{
		config: config,
	}
}
func (nr nodeRegistrar) nodeRegister() {
	// When kubeletRegistrationPath is specified then driver-registrar ONLY acts
	// as gRPC server which replies to registration requests initiated by kubelet's
	// plugins watcher infrastructure. Node labeling is done by kubelet's csi code.
	registrationServer := newRegistrationServer(nr.config.draDriverName, nr.config.draAddress, []string{"1.0.0"})
	socketPath := buildSocketPath(nr.config.draDriverName, nr.config.pluginRegistrationPath)
	if err := cleanupSocketFile(socketPath); err != nil {
		klog.Errorf("%+v", err)
		os.Exit(1)
	}

	var oldmask int
	if runtime.GOOS == "linux" {
		// Default to only user accessible socket, caller can open up later if desired
		oldmask, _ = umask(0077)
	}

	klog.Infof("Starting Registration Server at: %s\n", socketPath)
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		klog.Errorf("failed to listen on socket: %s with error: %+v", socketPath, err)
		os.Exit(1)
	}
	if runtime.GOOS == "linux" {
		umask(oldmask)
	}
	klog.Infof("Registration Server started at: %s\n", socketPath)
	grpcServer := grpc.NewServer()

	// Registers kubelet plugin watcher api.
	registerapi.RegisterRegistrationServer(grpcServer, registrationServer)

	go removeRegSocket(nr.config.draDriverName, nr.config.pluginRegistrationPath)
	// Starts service
	if err := grpcServer.Serve(lis); err != nil {
		klog.Errorf("Registration Server stopped serving: %v", err)
		os.Exit(1)
	}

	// If gRPC server is gracefully shutdown, cleanup and exit
	os.Exit(0)
}

func buildSocketPath(cdiDriverName string, pluginRegistrationPath string) string {
	return fmt.Sprintf("%s/%s-reg.sock", pluginRegistrationPath, cdiDriverName)
}

func removeRegSocket(cdiDriverName, pluginRegistrationPath string) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM)
	<-sigc
	socketPath := buildSocketPath(cdiDriverName, pluginRegistrationPath)
	err := os.Remove(socketPath)
	if err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to remove socket: %s with error: %+v", socketPath, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func umask(mask int) (int, error) {
	return unix.Umask(mask), nil
}

func cleanupSocketFile(socketPath string) error {
	socketExists, err := doesSocketExist(socketPath)
	if err != nil {
		return err
	}
	if socketExists {
		if err := os.Remove(socketPath); err != nil {
			return fmt.Errorf("failed to remove stale socket %s with error: %+v", socketPath, err)
		}
	}
	return nil
}

func doesSocketExist(socketPath string) (bool, error) {
	fi, err := os.Stat(socketPath)
	if err == nil {
		if isSocket := (fi.Mode()&os.ModeSocket != 0); isSocket {
			return true, nil
		}
		return false, fmt.Errorf("file exists in socketPath %s but it's not a socket.: %+v", socketPath, fi)
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to stat the socket %s with error: %+v", socketPath, err)
	}
	return false, nil
}
