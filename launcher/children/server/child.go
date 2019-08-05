package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
)

const (
	envListenerFileDescriptorKey = "LISTENER_FD_COUNT"
)

var (
	// it is to verify whether the current application did inherit listeners from the parent
	didInheritFromParent = os.Getenv(envListenerFileDescriptorKey) != ""
	// pid of parent process
	parentPID = os.Getppid()
)

type ChildServer struct {
	mutex *sync.Mutex
	inheritOnce *sync.Once
	listeners []net.Listener
	servers []*http.Server
	errors chan error
	hasStartedServing chan struct{}

	fileDescriptorStart int
}

func (c *ChildServer) listenAndServe() {
	err := c.inheritListener()
	if err != nil {
		panic(err)
	}

	for index := range c.servers {
		go func(index int) {
			c.errors <- c.servers[index].Serve(c.listeners[index])

		}(index)
	}

	go c.notifyParent()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		c.waitForShutdown()
	}()

	select {
	case err := <-c.errors:
		if err == nil {
			panic("unexpected nil error")
		}
	case <-waitDone:
		fmt.Println("Exiting process, pid: ", os.Getpid())
	}
}

func (c *ChildServer) waitForShutdown() {
	var wg sync.WaitGroup
	wg.Add(len(c.servers))
	go c.handleSignal(&wg)
	wg.Wait()
}

func (c *ChildServer) inheritListener() error {
	var err error
	c.inheritOnce.Do(func(){
		c.mutex.Lock()
		defer c.mutex.Unlock()
		countStr := os.Getenv(envListenerFileDescriptorKey)

		if countStr == "" {
			// no inheritance from the parent process
			return
		}
		count, atoiErr := strconv.Atoi(countStr)
		if atoiErr != nil {
			err = fmt.Errorf("invalid listener count: %s=%s", envListenerFileDescriptorKey, countStr)
			return
		}
		fdStart := c.fileDescriptorStart
		if fdStart == 0 {
			// set the file descriptor start from 3 since the listeners will begin at 3
			fdStart = 3
		}

		for i := fdStart; i < fdStart + count; i ++ {
			// for each active listener within this OS

			file := os.NewFile(uintptr(i), "listener") // create a new file to the listener (name doesn't matter)

			l, constructErr := net.FileListener(file) // re-construct back the socket listener in this process (the listener is "handed over" to this process)
			if constructErr != nil {
				_ = file.Close()
				err = fmt.Errorf("failed to inherit socket, file descriptor %d: %s", i, constructErr)
				return
			}
			closeErr := file.Close()
			if closeErr != nil {
				err = fmt.Errorf("failed to close inherited socket, file descriptor %d: %s", i, closeErr)
				return
			}
			c.listeners = append(c.listeners, NewListnerWrapper(c.hasStartedServing, l))
		}

	})
	return err
}

func (c *ChildServer) notifyParent() {
	startCount := 0
	for startCount < len(c.servers) {
		select {
		case <- c.hasStartedServing:
			startCount ++
		}
	}
	parent, err := os.FindProcess(parentPID)
	if err != nil {
		c.errors <- fmt.Errorf("failed to find parent process, err: %v", err)
		return
	}

	signalErr := parent.Signal(syscall.SIGUSR2)
	if signalErr != nil {
		c.errors <- fmt.Errorf("failed to send terminate signal to parent, err: %v", signalErr)
	}
}

func (c *ChildServer) terminate(wg *sync.WaitGroup) {
	for _, srv := range c.servers {
		go func(srv *http.Server) {
			defer wg.Done()
			if err := srv.Close(); err != nil {
				c.errors <- err
			}
		}(srv)
	}
}

func (c *ChildServer) handleSignal(wg *sync.WaitGroup) {
	// create a channel subscribing to system interrupts
	ch := make(chan os.Signal, 10)
	// SIGINT: user trigger INTR (Command + C), SIGTERM: signal of process termination, SIGUSR2: user reserved signal, default behaviour will be terminating the process
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	for {
		sig := <- ch
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			// stop subscribing to the terminate signal so that the child process can use it to terminate the parent process
			signal.Stop(ch)
			// shutdown the service
			c.terminate(wg)
			return
		}
	}

}

func (c *ChildServer) run() error {
	// getting listeners
	c.listenAndServe()
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		c.waitForShutdown()
	}()

	select {
	case err := <-c.errors:
		if err == nil {
			panic("unexpected nil error")
		}
		return err
	case <- waitDone:
		fmt.Println("Exiting process, pid: ", os.Getpid())
		return nil
	}
}

func NewServer(servers []*http.Server) *ChildServer {
	return &ChildServer{
		mutex: &sync.Mutex{},
		inheritOnce: &sync.Once{},
		servers: servers,
		listeners: make([]net.Listener, 0, len(servers)),
		errors: make(chan error, 1+len(servers)),
		hasStartedServing: make(chan struct{}, 1+ len(servers)),
	}
}

func Serve(servers ...*http.Server) error {
	s := NewServer(servers)
	return s.run()
}