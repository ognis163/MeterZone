package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// portBinder держит текущий HTTP-listener и умеет переехать на другой порт
// без перезапуска процесса: сначала открывается новый порт, затем закрывается старый.
type portBinder struct {
	mu      sync.Mutex
	host    string
	handler http.Handler
	ln      net.Listener
	port    int
	servers map[*http.Server]struct{}
}

func newPortBinder(host string, handler http.Handler) *portBinder {
	return &portBinder{
		host:    host,
		handler: handler,
		servers: make(map[*http.Server]struct{}),
	}
}

func (b *portBinder) Start(port int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.listenLocked(port)
}

func (b *portBinder) Change(port int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if port == b.port {
		return nil
	}
	if err := b.listenLocked(port); err != nil {
		return err
	}
	log.Printf("порт изменён на %d", port)
	return nil
}

func (b *portBinder) Port() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.port
}

func (b *portBinder) listenLocked(port int) error {
	addr := net.JoinHostPort(b.host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           b.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	old := b.ln
	b.ln = ln
	b.port = port
	b.servers[srv] = struct{}{}

	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Printf("HTTP-сервер, порт %d: %v", port, err)
		}
		b.mu.Lock()
		delete(b.servers, srv)
		b.mu.Unlock()
	}()

	if old != nil {
		_ = old.Close()
	}
	log.Printf("слушаю порт %d", port)
	return nil
}

func (b *portBinder) Shutdown(ctx context.Context) {
	b.mu.Lock()
	ln := b.ln
	b.ln = nil
	servers := make([]*http.Server, 0, len(b.servers))
	for s := range b.servers {
		servers = append(servers, s)
	}
	b.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, s := range servers {
		_ = s.Shutdown(ctx)
	}
}
