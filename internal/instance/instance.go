// Package instance keeps to one copy of the program running for each user. A
// copy started while another runs asks that one to show its window, and
// leaves, so that starting the program again from the desktop brings back the
// window there is, and two copies never write the same workspaces.
//
// The copy that runs holds a lock on a file, which the system lets go of if
// the program dies, and listens on a socket beside it. A copy that cannot
// take the lock sends the one that has it the desktop's activation token
// through the socket.
package instance

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrRunning is returned by Claim when another copy is running and has been
// asked to show its window.
var ErrRunning = errors.New("another copy of the program is running")

// ErrNoAnswer is returned by Claim when another copy holds the lock but does
// not answer, as one that hangs. This copy must not run beside it.
var ErrNoAnswer = errors.New("another copy of the program is running but does not answer")

// The names of the lock and the socket within the folder.
const (
	lockName   = "triggerapi.lock"
	socketName = "triggerapi.sock"
)

// answerWait is how long a copy started while another runs waits for that one
// to listen, as one that has just started may have the lock but not yet the
// socket. readWait is how long the copy that runs waits for a token once a
// connection is made; it is written at once.
var (
	answerWait = 5 * time.Second
	readWait   = time.Second
)

// maxToken is the most of a token that is read.
const maxToken = 4096

// Instance is the claim of the copy that runs.
type Instance struct {
	lock     *os.File
	listener net.Listener

	// mu guards closed, so that nothing is passed on once Close has
	// returned; conns are the connections being read, closed with it.
	mu     sync.Mutex
	closed bool
	conns  map[net.Conn]bool
	wg     sync.WaitGroup
}

// Claim makes this the copy that runs, with its lock and socket in dir, if no
// other copy is running. If one is, it is sent token, the desktop's leave to
// take the focus (which may be empty), and ErrRunning is returned, or
// ErrNoAnswer if it could not be.
func Claim(dir, token string) (*Instance, error) {
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	held, err := tryLock(lock)
	if err != nil {
		lock.Close()
		return nil, err
	}
	if !held {
		lock.Close()
		if err := handOver(filepath.Join(dir, socketName), token); err != nil {
			return nil, err
		}
		return nil, ErrRunning
	}

	// A socket left by a copy that died is in the way. Only the copy that
	// holds the lock takes it away, so it is never one in use.
	path := filepath.Join(dir, socketName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		lock.Close()
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		lock.Close()
		return nil, err
	}
	return &Instance{lock: lock, listener: listener, conns: map[net.Conn]bool{}}, nil
}

// handOver sends token to the copy that runs, waiting for it to listen.
func handOver(path, token string) error {
	deadline := time.Now().Add(answerWait)
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(answerWait))
			_, err = io.WriteString(conn, token+"\n")
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %w", ErrNoAnswer, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Serve calls activate, on a goroutine of its own, with the token of each copy
// started later, until Close. It returns at once.
func (i *Instance) Serve(activate func(token string)) {
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		for {
			conn, err := i.listener.Accept()
			if err != nil {
				return
			}
			i.mu.Lock()
			if i.closed {
				i.mu.Unlock()
				conn.Close()
				return
			}
			i.conns[conn] = true
			i.wg.Add(1)
			i.mu.Unlock()
			go i.read(conn, activate)
		}
	}()
}

// read reads the token from a copy started later and passes it on, unless the
// instance has been closed meanwhile.
func (i *Instance) read(conn net.Conn, activate func(token string)) {
	defer i.wg.Done()
	defer func() {
		i.mu.Lock()
		delete(i.conns, conn)
		i.mu.Unlock()
		conn.Close()
	}()
	conn.SetReadDeadline(time.Now().Add(readWait))
	line, err := bufio.NewReader(io.LimitReader(conn, maxToken)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.closed {
		activate(strings.TrimSpace(line))
	}
}

// Close stops listening and lets go of the claim, waiting for what Serve
// started; activate is not called once it has returned.
func (i *Instance) Close() error {
	i.mu.Lock()
	i.closed = true
	for conn := range i.conns {
		conn.Close()
	}
	i.mu.Unlock()
	err := i.listener.Close()
	i.wg.Wait()
	return errors.Join(err, i.lock.Close())
}
