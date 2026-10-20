package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ptyProc is a program running in a pseudo console of its own (ConPTY),
// in a job object that holds it and what it starts.
type ptyProc struct {
	mu      sync.Mutex
	console uintptr // the HPCON, 0 once closed
	in      *os.File
	out     *os.File
	process windows.Handle
	job     windows.Handle
	pid     int
	exited  chan struct{}
	code    int
}

type ptyOptions struct {
	Path       string
	Args       []string
	Dir        string
	Env        []string
	Cols, Rows int
}

var (
	kernel32                              = windows.NewLazySystemDLL("kernel32.dll")
	procCreatePseudoConsole               = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole               = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole                = kernel32.NewProc("ClosePseudoConsole")
	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")
)

const procThreadAttributePseudoConsole = 0x00020016

type startupInfoEx struct {
	windows.StartupInfo
	attributes uintptr
}

func startPTY(o ptyOptions) (*ptyProc, error) {
	if err := procCreatePseudoConsole.Find(); err != nil {
		return nil, errors.New("pty: ConPTY needs Windows 10 1809 or later")
	}
	// The console reads what is typed from inR and writes the output to
	// outW; the daemon writes to inW and reads outR.
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("pty: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, fmt.Errorf("pty: %w", err)
	}
	var console uintptr
	r, _, _ := procCreatePseudoConsole.Call(coord(o.Cols, o.Rows), uintptr(inR), uintptr(outW), 0, uintptr(unsafe.Pointer(&console)))
	// The console has its own copies of its ends.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if r != 0 {
		windows.CloseHandle(inW)
		windows.CloseHandle(outR)
		return nil, fmt.Errorf("pty: CreatePseudoConsole: %w", syscall.Errno(r&0xFFFF))
	}
	p := &ptyProc{
		console: console,
		in:      os.NewFile(uintptr(inW), "conpty-in"),
		out:     os.NewFile(uintptr(outR), "conpty-out"),
		exited:  make(chan struct{}),
	}
	if err := p.spawn(o); err != nil {
		p.Close()
		p.out.Close()
		return nil, err
	}
	go p.wait()
	return p, nil
}

func (p *ptyProc) spawn(o ptyOptions) error {
	var size uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	list := make([]byte, size)
	if r, _, err := procInitializeProcThreadAttributeList.Call(uintptr(unsafe.Pointer(&list[0])), 1, 0, uintptr(unsafe.Pointer(&size))); r == 0 {
		return fmt.Errorf("pty: InitializeProcThreadAttributeList: %w", err)
	}
	defer procDeleteProcThreadAttributeList.Call(uintptr(unsafe.Pointer(&list[0])))
	if r, _, err := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(&list[0])), 0, procThreadAttributePseudoConsole, p.console, unsafe.Sizeof(p.console), 0, 0); r == 0 {
		return fmt.Errorf("pty: UpdateProcThreadAttribute: %w", err)
	}
	si := startupInfoEx{attributes: uintptr(unsafe.Pointer(&list[0]))}
	si.Cb = uint32(unsafe.Sizeof(si))
	// The console's own standard handles: with the daemon's, which go to
	// its log, the program would write there instead.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput, si.StdOutput, si.StdErr = windows.InvalidHandle, windows.InvalidHandle, windows.InvalidHandle

	cmdline, err := windows.UTF16PtrFromString(commandLine(o.Path, o.Args))
	if err != nil {
		return err
	}
	var dir *uint16
	if o.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(o.Dir); err != nil {
			return err
		}
	}
	var env *uint16
	if o.Env != nil {
		block := environmentBlock(o.Env)
		env = &block[0]
	}
	// Suspended until it is in its job, so that whatever it starts is too.
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, flags, env, dir, &si.StartupInfo, &pi); err != nil {
		return fmt.Errorf("pty: starting %s: %w", o.Path, err)
	}
	if job, err := windows.CreateJobObject(nil, nil); err == nil {
		if windows.AssignProcessToJobObject(job, pi.Process) == nil {
			p.job = job
		} else {
			windows.CloseHandle(job)
		}
	}
	windows.ResumeThread(pi.Thread)
	windows.CloseHandle(pi.Thread)
	p.process, p.pid = pi.Process, int(pi.ProcessId)
	return nil
}

// wait waits for the program to exit, then closes the console: its output
// ends there, unlike a Unix terminal's, which ends as the last process
// holding it goes.
func (p *ptyProc) wait() {
	windows.WaitForSingleObject(p.process, windows.INFINITE)
	var code uint32
	windows.GetExitCodeProcess(p.process, &code)
	p.code = int(code)
	close(p.exited)
	p.Close()
}

// Read reads the program's output; io.EOF once the console is closed and
// wrote what it had.
func (p *ptyProc) Read(b []byte) (int, error) {
	n, err := p.out.Read(b)
	if err != nil && n == 0 && (errors.Is(err, os.ErrClosed) || errors.Is(err, windows.ERROR_BROKEN_PIPE)) {
		err = io.EOF
	}
	return n, err
}

func (p *ptyProc) Write(b []byte) (int, error) { return p.in.Write(b) }

// Resize resizes the console; ConPTY has no size in pixels.
func (p *ptyProc) Resize(cols, rows, _, _ int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.console == 0 {
		return os.ErrClosed
	}
	if r, _, _ := procResizePseudoConsole.Call(p.console, coord(cols, rows)); r != 0 {
		return fmt.Errorf("pty: ResizePseudoConsole: %w", syscall.Errno(r&0xFFFF))
	}
	return nil
}

func (p *ptyProc) PID() int { return p.pid }

// Close closes the console, which ends the processes attached to it, as a
// hangup does. Read returns io.EOF once it wrote what it had.
func (p *ptyProc) Close() error {
	p.mu.Lock()
	console := p.console
	p.console = 0
	p.mu.Unlock()
	if console == 0 {
		return nil
	}
	p.in.Close()
	// ClosePseudoConsole waits for its output to be read before Windows 11
	// 24H2: pump goes on reading meanwhile, to the end, then the pipe goes.
	go func() {
		procClosePseudoConsole.Call(console)
		p.out.Close()
	}()
	return nil
}

// Kill kills the program and whatever it started.
func (p *ptyProc) Kill() {
	if p.job == 0 || windows.TerminateJobObject(p.job, 1) != nil {
		windows.TerminateProcess(p.process, 1)
	}
}

// Foreground returns the program the shell runs: Windows has no process
// groups, so it is the one of the shell's children that started last,
// or, down its line of children, the coding agent one of them runs (as
// cmd.exe does for an npm shim); 0 when there is none.
func (p *ptyProc) Foreground() int {
	return foreground(p.pid)
}

// coord packs a COORD, which ConPTY takes by value.
func coord(cols, rows int) uintptr {
	return uintptr(uint16(max(1, min(cols, 0x7FFF)))) | uintptr(uint16(max(1, min(rows, 0x7FFF))))<<16
}

// commandLine joins a program and its arguments as the C runtime splits
// them; args[0] is the program's name, which Windows takes from the path.
func commandLine(path string, args []string) string {
	parts := []string{windows.EscapeArg(path)}
	if len(args) > 1 {
		for _, a := range args[1:] {
			parts = append(parts, windows.EscapeArg(a))
		}
	}
	return strings.Join(parts, " ")
}

// environmentBlock encodes env as CreateProcess takes it: NUL-terminated
// UTF-16 strings, then another NUL.
func environmentBlock(env []string) []uint16 {
	var b []uint16
	for _, kv := range env {
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	return append(b, 0)
}
