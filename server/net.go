package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"os"
)

const (
	// it is an environment variable indicates the number of file descriptor listeners inherited from the parent
	envListenerFileDescriptorKey = "LISTENER_FD_COUNT"
	envListenerFileDescriptorKeyPrefix = envListenerFileDescriptorKey + "="
)

var originalWD, _ = os.Getwd()

type Net struct {
	// listeners inherited from the parent process, map of address and listener
	inheritedListeners []net.Listener
	// listeners running on this process, map of address and listener
	activeListeners []net.Listener

	mutex sync.Mutex
	// to ensure retrieving listener only once
	inheritOnce sync.Once

	// the starting point of file descriptors for socket listeners, usually it will start from 3 (0,1,2 will be std.in, std.out and std.err)
	fileDescriptorStart int
}

func (n *Net) GetListener(nett, address string) (net.Listener, error) {
	switch nett {
	default:
		// not supporting other protocols
		return nil, net.UnknownNetworkError(nett)
	case "tcp", "tcp4", "tcp6":
		addr, resolveErr := net.ResolveTCPAddr(nett, address)
		if resolveErr != nil {
			return nil, resolveErr
		}
		return n.GetTCPListener(addr)
	case "unix", "unixpacket":
		addr, resolveErr := net.ResolveUnixAddr(nett, address)
		if resolveErr != nil {
			return nil, resolveErr
		}
		return n.GetUnixListener(addr)

	}
	return nil, nil
}

func (n *Net) GetTCPListener(address *net.TCPAddr) (net.Listener, error) {
	err := n.inheritListener()
	if err != nil {
		return nil, err
	}

	n.mutex.Lock()
	defer n.mutex.Unlock()

	for index, listener := range n.inheritedListeners {
		 if listener == nil {
		 	// ignore those not working/(taken away) listeners
		 	continue
		 }
		 if isEqual(listener.Addr(), address) {
		 	// move listener from inherited to active
		 	n.inheritedListeners[index] = nil
		 	n.activeListeners = append(n.activeListeners, listener)
		 	return listener.(*net.TCPListener), nil
		 }
	}

	// no such listener inherited

	return nil, nil
}

func (n *Net) GetUnixListener(address *net.UnixAddr) (net.Listener, error) {
	err := n.inheritListener()
	if err != nil {
		return nil, err
	}

	n.mutex.Lock()
	defer n.mutex.Unlock()

	// look for an inherited listener
	for index, listener := range n.inheritedListeners {
		if listener == nil {
			// ignore those not working/(taken away) listeners
			continue
		}
		if isEqual(listener.Addr(), address) {
			n.inheritedListeners[index] = nil
			n.activeListeners = append(n.activeListeners, listener)
			return listener.(*net.UnixListener), nil
		}
	}

	return nil, nil
}

func (n *Net) inheritListener() error {
	var err error
	n.inheritOnce.Do(func(){
		n.mutex.Lock()
		defer n.mutex.Unlock()
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
		fdStart := n.fileDescriptorStart
		if fdStart == 0 {
			// set the file descriptor start from 3 since the listeners will begin at 3
			fdStart = 3
		}

		for i := fdStart; i < fdStart + count; i ++ {
			// for each active listener within this OS

			file := os.NewFile(uintptr(i), "listener") // create a new file to the listener (name doesn't matter)

			l, constructErr := net.FileListener(file) // re-construct back the socket listener in this process (the listener is "handed over" to this process)
			if err != nil {
				_ = file.Close()
				err = fmt.Errorf("failed to inherit socket, file descriptor %d: %s", i, constructErr)
				return
			}

			closeErr := file.Close()
			if closeErr != nil {
				err = fmt.Errorf("failed to close inherited socket, file descriptor %d: %s", i, closeErr)
				return
			}
			n.inheritedListeners = append(n.inheritedListeners, l)
		}

	})

	return err
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

func (n *Net) getActiveListeners() []net.Listener {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	result := make([]net.Listener, len(n.activeListeners))
	copy(result, n.activeListeners)

	return result
}

// StartProcess starts a new child process passing its all active listeners with the same environment and arguments as this process was started. This allows a new version of binary to take over the same set of listeners of the current application. It will return the pid of the child process if the start is successful.
func (n *Net) StartProcess() (int, error) {

	listeners := n.getActiveListeners()
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
	// this can also be getting the path from any sources (env variable, searching, etc)
	binaryPath := "mainv2"

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
	newProcess, startErr := os.StartProcess(binaryPath, os.Args, &os.ProcAttr{
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

type filer interface {
	// a dummy interface which converts those listeners to File Descriptors
	// both net.TCPListener and net.UnixListener has the method File() (*os.File, error) but net.Listener interface doesn't have such method
	File() (*os.File, error)
}