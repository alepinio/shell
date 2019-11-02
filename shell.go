// Package shell provides interactive and persistent shell processes inside
// goroutines.
//
// It is built on top of package os/exec. A Unix system and a bash shell is
// assumed for this package to run correctly.
package shell

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// ProcessStopped is the error returned when Exec is called after Stop.
var ProcessStopped = errors.New("shell process already stopped")

// A Shell represents a shell process in preparation or execution, this last
// with or without jobs. It can receive one or many calls to its method Exec.
// The state of the shell process persists between calls to Exec, that is to
// say, after doing a call to Exec the next one happens in the state left by the
// previous call. A Shell cannot be used after calling its method Stop.
type Shell struct {
	c                *exec.Cmd
	stdinPipe        io.WriteCloser
	stdout           io.Writer
	stderr           io.Writer
	stdoutPipePath   string
	stderrPipePath   string
	exitCodePipePath string
	tempDirPath      string
	stdStreamCommCmd string
	exitCodeCommCmd  string
	wg               sync.WaitGroup
	wgcounter        int
}

// New returns a Shell struct ready to be used, where bin is the path to the
// shell executable to run, env the initial environment, dir the initial working
// directory and stdout and stderr where to redirect the standard output and
// error of the commands executed in the shell process.
func New(bin string, env []string, dir string, stdout, stderr io.Writer) *Shell {
	// Create an empty Shell
	s := Shell{}

	// Create an exec.Cmd for the shell process
	s.c = &exec.Cmd{
		Path:   bin,
		Args:   []string{bin},
		Env:    env,
		Dir:    dir,
		Stdout: nil,
		Stderr: nil,
	}

	// Set where to copy the standard output and standard error of the commands
	// executed in the shell process
	s.stdout = stdout
	s.stderr = stderr

	// Create a pipe to write to the standard input of the shell process from
	// the process where the Shell is
	if stdinPipe, err := s.c.StdinPipe(); err != nil {
		panic(err)
	} else {
		s.stdinPipe = stdinPipe
	}

	// Create a temporary directory where to put the named pipes that a Shell
	// use
	if tempDirPath, err := ioutil.TempDir("", "shell-named-pipes"); err != nil {
		panic(err)
	} else {
		s.tempDirPath = tempDirPath
	}

	// Create a 0600 (user can read, user can write) named pipes for the shell
	// process to communicate standard streams of executed commands to the
	// process where the Shell is
	if s.stdout != nil {
		s.stdoutPipePath = filepath.Join(s.tempDirPath, "stdout")
		if err := syscall.Mkfifo(s.stdoutPipePath, 0600); err != nil {
			panic(err)
		}
	}
	if s.stderr != nil {
		s.stderrPipePath = filepath.Join(s.tempDirPath, "stderr")
		if err := syscall.Mkfifo(s.stderrPipePath, 0600); err != nil {
			panic(err)
		}
	}

	// Create a 0600 (user can read, user can write) named pipe for the shell
	// process to communicate the exit code of executed commands to the process
	// where the Shell is
	s.exitCodePipePath = filepath.Join(s.tempDirPath, "exit_code")
	if err := syscall.Mkfifo(s.exitCodePipePath, 0600); err != nil {
		panic(err)
	}

	// Create command string for redirection of standard output and error of
	// executed commands to pipes
	if s.stdout != nil {
		s.stdStreamCommCmd += fmt.Sprintf("1>%s", s.stdoutPipePath)
	}
	if s.stderr != nil {
		s.stdStreamCommCmd += fmt.Sprintf(" 2>%s", s.stderrPipePath)
	}

	// Create exit code communication command string
	s.exitCodeCommCmd = fmt.Sprintf("echo $? 1>%s", s.exitCodePipePath)

	// Set value for wait group counter (exit code is always communicated to the
	// process where the Shell is)
	s.wgcounter += 1
	if s.stdout != nil {
		s.wgcounter += 1
	}
	if s.stderr != nil {
		s.wgcounter += 1
	}

	// Return a Shell ready to use
	return &s
}

// Exec executes the command cmd in the shell process s and returns the
// corresponding exit code.
func (s *Shell) Exec(cmd string) int {
	// Throw a meaningful error if Stop was already called. If the temporary
	// directory does not exist, then it is supossed that the shell process
	// was stopped
	if _, err := os.Stat(s.tempDirPath); os.IsNotExist(err) {
		panic(err)
	}

	// Start the shell process if it was not started yet (only happens in first
	// call to Exec)
	if s.c.Process == nil {
		s.start()
	}

	// Append interprocess communication paraphernalia to the command to execute
	cmd2 := fmt.Sprintf("%s %s ; %s\n", cmd, s.stdStreamCommCmd, s.exitCodeCommCmd)

	// Initialize wait group
	s.wg.Add(s.wgcounter)

	// Copy data from the stdout pipe (if the pipe is empty os.Open will block
	// until someone writes to the pipe and closes it; if the pipe is being
	// written os.Open will block until the one writing finishes and closes the
	// pipe) to the process where the Shell is
	if s.stdout != nil {
		go copyFromPipe(s.stdoutPipePath, s.stdout, &s.wg)
	}

	// Copy data from the stderr pipe to the process where the Shell is
	if s.stderr != nil {
		go copyFromPipe(s.stderrPipePath, s.stderr, &s.wg)
	}

	// Copy data from exit code pipe to the process where the Shell is
	var exitCodeBuf strings.Builder
	go copyFromPipe(s.exitCodePipePath, &exitCodeBuf, &s.wg)

	// Send command to shell process (it is executed when shell process reads
	// newline character)
	io.WriteString(s.stdinPipe, cmd2)

	// Wait until all data is copied from pipes
	s.wg.Wait()

	// Trim newline character in exit code pipe data
	exitCodeString := strings.TrimSpace(exitCodeBuf.String())

	// Convert exit code string to int
	exitCode, err := strconv.Atoi(exitCodeString)
	if err != nil {
		panic(err)
	}

	return exitCode
}

// Stop stops the shell process s and releases the resources associated with it.
func (s *Shell) Stop() {
	// Throw a meaningful error if Stop was already called. If the temporary
	// directory does not exist, then it is supossed that the shell process
	// was stopped
	if _, err := os.Stat(s.tempDirPath); os.IsNotExist(err) {
		panic(ProcessStopped)
	}

	// Remove the temporary directory where named pipes were put
	if err := os.RemoveAll(s.tempDirPath); err != nil {
		panic(err)
	}

	// Close stdin pipe (exec.Cmd.Wait will wait forever if stdin pipe is not
	// closed)
	if err := s.stdinPipe.Close(); err != nil {
		panic(err)
	}

	// Wait for the stdin pipe to be closed, any copying from stdout or stderr
	// to complete, and release resources associated with the exec Cmd. Do not
	// wait if shell process was not started (say, if no call to Exec was done)
	if s.c.Process != nil {
		if err := s.c.Wait(); err != nil {
			panic(err)
		}
	}
}

// copyFromPipe copies data from a named pipe and tells wait group when
// finishes
func copyFromPipe(pipePath string, writer io.Writer, wg *sync.WaitGroup) {
	defer wg.Done()
	pipe, err := os.Open(pipePath)
	if err != nil {
		panic(err)
	}
	io.Copy(writer, pipe)
	pipe.Close()
}

// start starts the shell process s.
func (s *Shell) start() {
	// Start exec.Cmd
	if err := s.c.Start(); err != nil {
		panic(err)
	}
}
