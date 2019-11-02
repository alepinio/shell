package shell_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/alepinio/shell"
)

func Example_1() {
	s := shell.New("/bin/bash", nil, "/", os.Stdout, nil)

	s.Exec("echo foo")
	s.Stop()

	// Output: foo
}

func Example_2() {
	var buf bytes.Buffer
	s := shell.New("/bin/bash", nil, "/", &buf, nil)

	s.Exec("cd tmp")
	s.Exec("pwd")
	s.Stop()

	fmt.Print(buf.String())
	// Output: /tmp
}

func Example_3() {
	var buf bytes.Buffer
	s := shell.New("/bin/bash", []string{"FOO=0"}, "/", &buf, nil)

	s.Exec("export FOO=1")
	s.Exec("echo $FOO")
	s.Stop()

	fmt.Print(buf.String())
	// Output: 1
}

func Example_4() {
	var buf bytes.Buffer
	s := shell.New("/bin/bash", nil, "/", nil, &buf)

	s.Exec("man")
	s.Stop()

	fmt.Print(buf.String())
	// Output: What manual page do you want?
}

func Example_5() {
	var buf bytes.Buffer
	var buff bytes.Buffer
	s := shell.New("/bin/bash", nil, "/", &buf, &buff)

	s.Exec("echo foo")
	s.Exec("man")
	s.Stop()

	fmt.Print(strings.TrimSpace(buf.String()), ", ", buff.String())
	// Output: foo, What manual page do you want?
}

func Example_6() {
	s := shell.New("/bin/bash", nil, "/", nil, nil)

	exitCode := s.Exec("test 1 -le 2")
	s.Stop()

	fmt.Print(exitCode)
	// Output: 0
}
