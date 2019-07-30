package server

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	// it is to verify whether the current application did inherit listeners from the parent
	didInheritFromParent = os.Getenv(envListenerFileDescriptorKey) != ""
	// pid of parent process
	parentPID = os.Getppid()
)

type Server struct {
	network *Net
	servers []*http.Server
	listeners []net.Listener
	//
	errors chan error
}

func NewServer(servers []*http.Server) *Server {
	return &Server{
		servers: servers,
		network: &Net{},
		listeners: make([]net.Listener, 0, len(servers)),
		errors: make(chan error, 1+len(servers)),
	}
}

func (s *Server) listen() error {
	for _, srv := range s.servers {
		var listener net.Listener
		l, err := s.network.GetListener("tcp", srv.Addr)
		if err != nil {
			panic(err)
			return err
		}
		if l == nil && didInheritFromParent {
			// unable to inherit the listener from the parent process
			return fmt.Errorf("failed to inherit listener from parent, address:%s, self:%d", srv.Addr, os.Getpid())
		}
		if l != nil {
			// listener inherit from the parent process
			listener = l
		} else {
			// there is no listener can inherit
			list, createErr := net.Listen("tcp", srv.Addr)
			if createErr != nil {
				return createErr
			}
			listener = list
		}
		if srv.TLSConfig != nil {
			listener = tls.NewListener(listener, srv.TLSConfig)
		}
		s.listeners = append(s.listeners, listener)
	}

	for _, l := range s.listeners {
		s.network.activeListeners = append(s.network.activeListeners, l)
	}
	return nil
}

func (s *Server) terminate(wg *sync.WaitGroup) {
	for _, srv := range s.servers {
		go func(srv *http.Server) {
			defer wg.Done()
			if err := srv.Close(); err != nil {
				s.errors <- err
			}
		}(srv)
	}
}

func (s *Server) serve() {
	for index, srv := range s.servers {
		go func() {
			s.errors <- srv.Serve(s.listeners[index])

		}()
	}
}

func (s *Server) handleSignal(wg *sync.WaitGroup) {
	// create a channel subscribing to system interrupts
	ch := make(chan os.Signal, 10)
	// SIGINT: user trigger INTR (Command + C), SIGTERM: signal of process termination, SIGUSR2: user reserved signal, default behaviour will be terminating the process
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR2)

	for {
		sig := <- ch
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			// stop subscribing to the terminate signal so that the child process can use it to terminate the parent process
			signal.Stop(ch)
			// shutdown the service
			s.terminate(wg)
			return
		case syscall.SIGUSR2:
			// initiate the child process
			_, err := s.network.StartProcess()
			if err != nil {
				s.errors <- err
			}
		}
	}
}

func (s *Server) run() error {
	// getting listeners
	err := s.listen()
	if err != nil {
		return err
	}
	if !didInheritFromParent {
		const msg = "Serving %s with pid %d\n"
		fmt.Printf(msg, pprintAddr(s.listeners), os.Getpid())
	} else if parentPID == 1 {
		// successfully killed parent
		fmt.Printf("Listening on init activated %s\n", pprintAddr(s.listeners))
	} else {
		// handover is ongoing
		const msg = "Graceful handoff of %s with new pid %d and old pid %d\n"
		fmt.Printf(msg, pprintAddr(s.listeners), os.Getpid(), parentPID)
	}

	// start serving
	s.serve()
	if didInheritFromParent && parentPID != 1 {
		// Close the parent if we inherited and it wasn't init that started us.
		closeErr := syscall.Kill(parentPID, syscall.SIGTERM)
		if closeErr != nil {
			return fmt.Errorf("failed to close parent process, err: %v", closeErr)
		}
	}
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		s.waitForShutdown()
	}()

	select {
	case err := <-s.errors:
		if err == nil {
			panic("unexpected nil error")
		}
		return err
	case <- waitDone:
		fmt.Println("Exiting process, pid: ", os.Getpid())
		return nil
	}
}

func (s *Server) waitForShutdown() {
	var wg sync.WaitGroup
	wg.Add(len(s.servers))
	go s.handleSignal(&wg)
	wg.Wait()
}

// Used for pretty printing addresses.
func pprintAddr(listeners []net.Listener) []byte {
	var out bytes.Buffer
	for i, l := range listeners {
		if i != 0 {
			fmt.Fprint(&out, ", ")
		}

		fmt.Fprint(&out, l.Addr())
	}
	return out.Bytes()
}

// Serve will serve the given http.Servers and will monitor for signals
// allowing for graceful termination (SIGTERM) or restart (SIGUSR2).
func Serve(servers ...*http.Server) error {
	s := NewServer(servers)
	return s.run()
}
