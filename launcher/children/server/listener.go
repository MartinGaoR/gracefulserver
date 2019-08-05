package server

import (
	"net"
	"sync"
)

type ListenerWrapper struct {
	startedOnce *sync.Once
	L net.Listener
	hasStartedAccepting chan struct{}
}

func (l *ListenerWrapper) Accept() (net.Conn, error) {
	l.startedOnce.Do(func(){
		l.hasStartedAccepting <- struct{}{}
	})
	return l.L.Accept()
}

func (l *ListenerWrapper) Close() error {
	return l.L.Close()
}

func (l *ListenerWrapper) Addr() net.Addr {
	return l.L.Addr()
}

func NewListnerWrapper(c chan struct{}, l net.Listener) net.Listener {
	return &ListenerWrapper{
		startedOnce: &sync.Once{},
		L: l,
		hasStartedAccepting: c,
	}
}