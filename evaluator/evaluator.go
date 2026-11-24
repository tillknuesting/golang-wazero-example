// This program is the "guest" half of the example: host.go runs it inside the
// wazero WebAssembly runtime.
//
// Nothing in this file is specific to WebAssembly. It is an ordinary Go
// program that reads its arguments and standard input and writes to standard
// output, and it runs natively too:
//
//	printf 'a\nb\n' | go run ./evaluator hello
//
// What makes it WebAssembly is how it is built (`go generate` in the
// repository root runs this command):
//
//	GOOS=wasip1 GOARCH=wasm go build -trimpath -buildvcs=false -o evaluator/evaluator.wasm ./evaluator
//
// GOARCH=wasm makes the compiler emit WebAssembly bytecode instead of machine
// code for a physical CPU. GOOS=wasip1 makes WASI preview 1 the "operating
// system": wherever the standard library would make a system call, it calls a
// function imported from the host instead. For this program:
//
//	os.Args               -> args_sizes_get, args_get
//	reading os.Stdin      -> fd_read  (file descriptor 0)
//	writing to os.Stdout  -> fd_write (file descriptor 1)
//	writing to os.Stderr  -> fd_write (file descriptor 2)
//	main returns          -> proc_exit(0)
//	os.Exit(n)            -> proc_exit(n)
//	an unrecovered panic  -> message on stderr, then proc_exit(2)
//
// The host provides those functions (wazero does, in host.go), so the host
// decides what the arguments are, where input comes from and where output
// goes. The guest cannot reach anything the host did not hand it.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	// os.Args comes from the host: host.go passes ["evaluator",
	// "valuefromargs"] with ModuleConfig.WithArgs. Without an argument, fall
	// back to "0".
	state := "0"
	if len(os.Args) > 1 {
		state = os.Args[1]
	}

	// os.Stdout has no buffer, so each Println is one fd_write call and the
	// host receives the line immediately.
	fmt.Println("From Args:", state)

	// Read standard input line by line until it ends.
	//
	// When the scanner needs more input it reads from os.Stdin, which calls
	// fd_read. wazero implements that by calling Read on the host's pipe,
	// which blocks until the host writes the next line. Go's wasip1 port runs
	// all goroutines on a single thread, so the whole guest is paused during
	// that call, and it uses no CPU while it waits.
	//
	// When the host closes its end of the pipe, the read reports EOF, Scan
	// returns false, and the loop ends.
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println("From Stdin:", scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		// os.Stderr is file descriptor 2, which host.go connects to the host's
		// own stderr.
		fmt.Fprintln(os.Stderr, "reading standard input:", err)
		// A non-zero exit code reaches the host as a *sys.ExitError, so the
		// host can tell that something went wrong.
		os.Exit(1)
	}

	// Returning from main makes the Go runtime call proc_exit(0). wazero
	// treats exit code 0 as success, so the host's InstantiateModule call
	// returns without an error.
}
