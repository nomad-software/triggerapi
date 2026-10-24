//go:build unix

package instance

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// tempDir is a folder for a lock and socket. A socket's path is limited to
// about a hundred bytes, which t.TempDir, named after the test, can pass.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "inst")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// claim claims dir, failing the test if it cannot, and serves into a channel.
func claim(t *testing.T, dir string) (*Instance, chan string) {
	t.Helper()
	i, err := Claim(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	tokens := make(chan string, 10)
	i.Serve(func(token string) { tokens <- token })
	t.Cleanup(func() { i.Close() })
	return i, tokens
}

// recv waits for a token.
func recv(t *testing.T, tokens chan string) string {
	t.Helper()
	select {
	case token := <-tokens:
		return token
	case <-time.After(5 * time.Second):
		t.Fatal("no token was passed on")
		return ""
	}
}

func TestSecondCopyHandsOver(t *testing.T) {
	dir := tempDir(t)
	_, tokens := claim(t, dir)

	if _, err := Claim(dir, "token-1"); !errors.Is(err, ErrRunning) {
		t.Fatalf("second claim = %v", err)
	}
	if got := recv(t, tokens); got != "token-1" {
		t.Errorf("token = %q", got)
	}

	// A copy started with no token still has the window shown.
	if _, err := Claim(dir, ""); !errors.Is(err, ErrRunning) {
		t.Fatalf("third claim = %v", err)
	}
	if got := recv(t, tokens); got != "" {
		t.Errorf("token = %q", got)
	}
}

func TestClaimAfterClose(t *testing.T) {
	dir := tempDir(t)
	i, err := Claim(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	again, tokens := claim(t, dir)
	if again == nil {
		t.Fatal("not claimed again")
	}
	if _, err := Claim(dir, "x"); !errors.Is(err, ErrRunning) {
		t.Fatalf("claim = %v", err)
	}
	recv(t, tokens)
}

// TestStaleSocket checks that a socket left by a copy that died, which holds
// no lock, is taken over rather than taken for a copy that runs.
func TestStaleSocket(t *testing.T) {
	dir := tempDir(t)
	listener, err := net.Listen("unix", filepath.Join(dir, socketName))
	if err != nil {
		t.Fatal(err)
	}
	// The socket's file stays, as when its program is killed.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	listener.Close()

	_, tokens := claim(t, dir)
	if _, err := Claim(dir, "after"); !errors.Is(err, ErrRunning) {
		t.Fatalf("claim = %v", err)
	}
	if got := recv(t, tokens); got != "after" {
		t.Errorf("token = %q", got)
	}
}

// TestHandOverWaitsForListening checks that a copy started just after another,
// which holds the lock but does not yet listen, waits for it.
func TestHandOverWaitsForListening(t *testing.T) {
	dir := tempDir(t)
	lock := holdLock(t, dir)
	defer lock.Close()

	got := make(chan string, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		listener, err := net.Listen("unix", filepath.Join(dir, socketName))
		if err != nil {
			got <- err.Error()
			return
		}
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			got <- err.Error()
			return
		}
		defer conn.Close()
		buf := make([]byte, 100)
		n, _ := conn.Read(buf)
		got <- string(buf[:n])
	}()

	if _, err := Claim(dir, "late"); !errors.Is(err, ErrRunning) {
		t.Fatalf("claim = %v", err)
	}
	if s := <-got; s != "late\n" {
		t.Errorf("sent %q", s)
	}
}

// TestNoAnswer checks that a copy started while another holds the lock but
// never listens says so, rather than running beside it.
func TestNoAnswer(t *testing.T) {
	old := answerWait
	answerWait = 200 * time.Millisecond
	defer func() { answerWait = old }()

	dir := tempDir(t)
	lock := holdLock(t, dir)
	defer lock.Close()

	i, err := Claim(dir, "")
	if !errors.Is(err, ErrNoAnswer) || i != nil {
		t.Fatalf("claim = %v, %v", i, err)
	}
}

// TestIdleConnection checks that a connection that sends nothing holds up
// neither the copies started after it nor Close.
func TestIdleConnection(t *testing.T) {
	dir := tempDir(t)
	i, tokens := claim(t, dir)

	idle, err := net.Dial("unix", filepath.Join(dir, socketName))
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()

	if _, err := Claim(dir, "next"); !errors.Is(err, ErrRunning) {
		t.Fatalf("claim = %v", err)
	}
	if got := recv(t, tokens); got != "next" {
		t.Errorf("token = %q", got)
	}

	start := time.Now()
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > readWait/2 {
		t.Errorf("Close took %v", took)
	}
}

func TestDirFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", tmp)

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != tmp || info.Mode().Perm() != 0o700 {
		t.Errorf("dir = %s, %v", dir, info.Mode())
	}
	// It is used again.
	if again, err := Dir(); err != nil || again != dir {
		t.Errorf("again = %s, %v", again, err)
	}

	// One that others can open, as another user could have made, is not
	// used.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(); err == nil {
		t.Error("a folder others can open was used")
	}
}

func TestDirRuntime(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if dir, err := Dir(); err != nil || dir != "/run/user/1000" {
		t.Errorf("dir = %s, %v", dir, err)
	}
}

// holdLock takes the lock in dir as another copy would.
func holdLock(t *testing.T, dir string) *os.File {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return f
}
