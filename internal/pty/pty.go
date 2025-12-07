package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/xunzhou/muxctl/internal/debug"
	"golang.org/x/sys/unix"
)

// PTY represents a pseudo-terminal pair (master/slave).
type PTY struct {
	Master      *os.File
	Slave       *os.File
	masterFd    int
	slaveFd     int
	rows        int
	cols        int
	closed      bool
	outputChan  chan []byte
	errorChan   chan error
	stopReadCh  chan struct{}
	tmuxProcess *os.Process
}

// New allocates a new PTY pair using openpty().
// Returns a PTY with master and slave file descriptors.
func New(rows, cols int) (*PTY, error) {
	debug.Log("PTY.New: allocating PTY rows=%d cols=%d", rows, cols)

	var masterFd, slaveFd int
	var winsize unix.Winsize

	winsize.Row = uint16(rows)
	winsize.Col = uint16(cols)

	// Call openpty(3) via syscall
	// openpty(int *amaster, int *aslave, char *name, struct termios *termp, struct winsize *winp)
	//
	// We'll use a simpler approach: open /dev/ptmx and unlockpt
	masterFile, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/ptmx: %w", err)
	}

	masterFd = int(masterFile.Fd())

	// Grant access to slave
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(masterFd), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&slaveFd))); errno != 0 {
		masterFile.Close()
		return nil, fmt.Errorf("ioctl TIOCGPTN failed: %v", errno)
	}

	// Unlock slave
	unlock := 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(masterFd), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		masterFile.Close()
		return nil, fmt.Errorf("ioctl TIOCSPTLCK failed: %v", errno)
	}

	// Get slave path
	slavePath := fmt.Sprintf("/dev/pts/%d", slaveFd)
	slaveFile, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		masterFile.Close()
		return nil, fmt.Errorf("failed to open slave %s: %w", slavePath, err)
	}

	slaveFd = int(slaveFile.Fd())

	// Set window size on master
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(masterFd), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&winsize))); errno != 0 {
		masterFile.Close()
		slaveFile.Close()
		return nil, fmt.Errorf("ioctl TIOCSWINSZ failed: %v", errno)
	}

	debug.Log("PTY.New: allocated master_fd=%d slave=%s", masterFd, slavePath)

	return &PTY{
		Master:     masterFile,
		Slave:      slaveFile,
		masterFd:   masterFd,
		slaveFd:    slaveFd,
		rows:       rows,
		cols:       cols,
		outputChan: make(chan []byte, 256),
		errorChan:  make(chan error, 1),
		stopReadCh: make(chan struct{}),
	}, nil
}

// SpawnTmux starts a tmux server attached to the PTY slave.
// The tmux server uses the slave as its controlling terminal.
func (p *PTY) SpawnTmux(socketPath, sessionName string) error {
	debug.Log("PTY.SpawnTmux: socket=%s session=%s", socketPath, sessionName)

	// Build tmux command:
	// tmux -S <socket> new-session -A -D -s <session> -x <cols> -y <rows>
	//
	// Flags:
	//   -S <socket>: use Unix socket at this path
	//   new-session: create new session
	//   -A: attach if exists, otherwise create
	//   -D: detach other clients (for embedded use)
	//   -s <name>: session name
	//   -x <cols>: width
	//   -y <rows>: height

	args := []string{
		"-S", socketPath,
		"new-session",
		"-A",  // attach or create
		"-D",  // detach others
		"-s", sessionName,
		"-x", fmt.Sprintf("%d", p.cols),
		"-y", fmt.Sprintf("%d", p.rows),
	}

	cmd := exec.Command("tmux", args...)

	// Set the slave PTY as stdin/stdout/stderr for tmux
	cmd.Stdin = p.Slave
	cmd.Stdout = p.Slave
	cmd.Stderr = p.Slave

	// Set controlling terminal (slave PTY)
	// Ctty: 0 means use stdin (which we set to p.Slave above)
	// This is the fd number in the child process, not parent
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0, // stdin in child process
	}

	// Start tmux
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tmux: %w", err)
	}

	p.tmuxProcess = cmd.Process

	debug.Log("PTY.SpawnTmux: tmux started with PID=%d", cmd.Process.Pid)

	// Start background goroutine to wait for tmux exit
	go func() {
		err := cmd.Wait()
		if err != nil {
			debug.Log("PTY.SpawnTmux: tmux exited with error: %v", err)
			p.errorChan <- fmt.Errorf("tmux process exited: %w", err)
		} else {
			debug.Log("PTY.SpawnTmux: tmux exited normally")
			p.errorChan <- fmt.Errorf("tmux process exited")
		}
	}()

	// Close slave fd in parent process (tmux has it open)
	// This is important: we only read/write from master
	p.Slave.Close()
	p.Slave = nil

	return nil
}

// StartReadLoop starts a goroutine that reads from PTY master and sends output to channel.
// Buffer size is 64 KiB as per spec.
func (p *PTY) StartReadLoop() {
	debug.Log("PTY.StartReadLoop: starting")

	go func() {
		buf := make([]byte, 64*1024) // 64 KiB buffer

		for {
			select {
			case <-p.stopReadCh:
				debug.Log("PTY.StartReadLoop: stopped")
				return
			default:
			}

			n, err := p.Master.Read(buf)
			if err != nil {
				if err == io.EOF || p.closed {
					debug.Log("PTY.StartReadLoop: EOF or closed")
					p.errorChan <- io.EOF
					return
				}
				debug.Log("PTY.StartReadLoop: read error: %v", err)
				p.errorChan <- err
				return
			}

			if n > 0 {
				// Copy buffer to avoid race with next read
				data := make([]byte, n)
				copy(data, buf[:n])

				select {
				case p.outputChan <- data:
				case <-p.stopReadCh:
					return
				}
			}
		}
	}()
}

// Write sends data to the PTY master (user input to tmux).
func (p *PTY) Write(data []byte) (int, error) {
	if p.closed {
		return 0, fmt.Errorf("PTY closed")
	}
	return p.Master.Write(data)
}

// WriteString is a convenience method to write strings.
func (p *PTY) WriteString(s string) (int, error) {
	return p.Write([]byte(s))
}

// OutputChan returns the channel that receives PTY output data.
func (p *PTY) OutputChan() <-chan []byte {
	return p.outputChan
}

// ErrorChan returns the channel that receives PTY errors.
func (p *PTY) ErrorChan() <-chan error {
	return p.errorChan
}

// Resize changes the PTY dimensions and notifies tmux via TIOCSWINSZ ioctl.
func (p *PTY) Resize(rows, cols int) error {
	if p.closed {
		return fmt.Errorf("PTY closed")
	}

	debug.Log("PTY.Resize: rows=%d cols=%d (was %dx%d)", rows, cols, p.rows, p.cols)

	var winsize unix.Winsize
	winsize.Row = uint16(rows)
	winsize.Col = uint16(cols)

	// Send TIOCSWINSZ ioctl to master FD
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(p.masterFd), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&winsize))); errno != 0 {
		return fmt.Errorf("ioctl TIOCSWINSZ failed: %v", errno)
	}

	p.rows = rows
	p.cols = cols

	return nil
}

// GetSize returns the current PTY dimensions.
func (p *PTY) GetSize() (rows, cols int) {
	return p.rows, p.cols
}

// Close closes the PTY master and stops the read loop.
// This will also cause tmux to exit.
func (p *PTY) Close() error {
	if p.closed {
		return nil
	}

	debug.Log("PTY.Close: closing PTY")

	p.closed = true

	// Stop read loop
	close(p.stopReadCh)

	// Kill tmux process if still running
	if p.tmuxProcess != nil {
		debug.Log("PTY.Close: killing tmux process PID=%d", p.tmuxProcess.Pid)
		p.tmuxProcess.Kill()
	}

	// Close master (slave was already closed in SpawnTmux)
	if p.Master != nil {
		p.Master.Close()
		p.Master = nil
	}

	return nil
}
