package launcher

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

const (
	envListenerFileDescriptorKey = "LISTENER_FD_COUNT"
	envListenerFileDescriptorKeyPrefix = envListenerFileDescriptorKey + "="
)

var originalWD, _ = os.Getwd()

type Launcher struct{
	mutex *sync.Mutex
	wg *sync.WaitGroup
	child int
	Listeners []net.Listener
	activeListenerCount int
	done chan struct{}
	processBuffer chan int
	deploy chan string
	errors chan error
}

func NewLauncher(wg *sync.WaitGroup, dp  chan string, done chan struct{}, err chan error) Launcher {
	return Launcher{
		mutex: &sync.Mutex{},
		wg: wg,
		Listeners: []net.Listener{},
		done: done,
		deploy: dp,
		errors: err,
		// non-buffered channel ensures that at most 2 applications can be run at the same time
		processBuffer: make(chan int),
	}
}

func (l *Launcher) AddListener(address string) error {
	for _, listener := range l.Listeners {
		if listener == nil {
			// ignore those not working/(taken away) listeners
			continue
		}
		adr, err := net.ResolveTCPAddr("tcp", address)
		if err != nil {
			return err
		}

		if isEqual(listener.Addr(), adr) {
			// do nothing
			return nil
		}
	}
	lis, constructErr := net.Listen("tcp", address)
	if constructErr != nil {
		return constructErr
	}
	if lis == nil {
		return fmt.Errorf("failed to construct listener")
	}
	l.Listeners = append(l.Listeners, lis)
	return nil
}

// isEqual is to compare 2 network address and return true if they are equal
func isEqual(addr1, addr2 net.Addr) bool {
	if addr1.Network() != addr2.Network() {
		return false
	}
	a1Str := addr1.String()
	a2Str := addr2.String()
	if a1Str == a2Str {
		return true
	}
	// This allows for ipv6 vs ipv4 local addresses to compare as equal. This
	// scenario is common when listening on localhost.
	const ipv6prefix = "[::]"
	a1Str = strings.TrimPrefix(a1Str, ipv6prefix)
	a2Str = strings.TrimPrefix(a2Str, ipv6prefix)
	const ipv4prefix = "0.0.0.0"
	a1Str = strings.TrimPrefix(a1Str, ipv4prefix)
	a2Str = strings.TrimPrefix(a2Str, ipv4prefix)
	return a1Str == a2Str
}

func (l *Launcher) getActiveListeners() []net.Listener {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	result := make([]net.Listener, len(l.Listeners))
	copy(result, l.Listeners)

	return result
}

func (l *Launcher) startChildProcess(binaryName string) (int, error) {
	listeners := l.getActiveListeners()
	var err error
	// getting the file descriptors from each of the active listeners
	files := make([]*os.File, len(listeners))
	defer func(){
		// a simple clean up of open files
		for _, f := range files {
			if f != nil {
				_ = f.Close()
			}
		}
	}()
	for index, listener := range listeners {
		// write listener file descriptors to files
		files[index], err = listener.(filer).File()
		if err != nil {
			return 0, err
		}
	}

	// copying the environment
	var env []string
	for _, v := range os.Environ() {
		// skip copying the listener count
		if strings.HasPrefix(v, envListenerFileDescriptorKeyPrefix) {
			continue
		}
		env = append(env, v)
	}

	// update the number of listeners
	env = append(env, fmt.Sprintf("%s%d", envListenerFileDescriptorKeyPrefix, len(listeners)))

	// collect all listeners
	allFiles := append([]*os.File{os.Stdin, os.Stdout, os.Stderr}, files ...)

	// start the new process with the same environment and arguments
	newProcess, startErr := os.StartProcess(binaryName, os.Args, &os.ProcAttr{
		// Dir can be the same or different directory
		Dir:   originalWD,
		Env: env,
		Files: allFiles,
	})

	if startErr != nil {
		return 0, startErr
	}
	return newProcess.Pid, nil
}

func (l *Launcher) Run() {
	go l.handleSignal()
	for {
		select {
		case adr := <- l.deploy: {
			l.deployBinary(adr)
		}
		case <- l.done: {
			l.terminate()
			return
		}
		}
	}
}

func (l *Launcher) deployBinary(binary string) {
	l.wg.Add(1)
	// need to record the old pid
	newPid, err := l.startChildProcess(binary)
	if err != nil {
		l.errors <- err
		return
	}
	l.processBuffer <- newPid
}

func (l *Launcher) terminate() {
	err := syscall.Kill(l.child, syscall.SIGINT)
	if err != nil {
		l.errors <- err
	}
	l.wg.Done()
}

func (l *Launcher) handleSignal() {
	// create a channel subscribing to system interrupts
	ch := make(chan os.Signal, 10)
	signal.Notify(ch, syscall.SIGUSR2)

	for {
		sig := <- ch
		switch sig {
		case syscall.SIGUSR2:
			if l.child == 0 {
				// the first version of deployment, need to catch the child process id
				l.child = <- l.processBuffer
				continue
			}
			l.terminate()
			// pick up the new process id
			l.child = <- l.processBuffer
		}
	}

}



type filer interface {
	// a dummy interface which converts those listeners to File Descriptors
	// both net.TCPListener and net.UnixListener has the method File() (*os.File, error) but net.Listener interface doesn't have such method
	File() (*os.File, error)
}