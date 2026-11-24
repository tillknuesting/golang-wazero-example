// This program is the "host" half of a small example of running WebAssembly
// from Go. The "guest" half is evaluator/evaluator.go.
//
// Both halves are ordinary Go. What differs is how they are built:
//
//   - The host (this file) is compiled for your machine like any Go program.
//     It embeds wazero (https://wazero.io), a WebAssembly runtime written in
//     pure Go: no cgo, and nothing extra to install.
//   - The guest is compiled with GOOS=wasip1 GOARCH=wasm into
//     evaluator/evaluator.wasm, portable bytecode that a WebAssembly runtime
//     executes inside a sandbox.
//
// When you run it, the host starts the guest with one command-line argument,
// feeds ten lines into the guest's standard input (one per second), and
// prints each line the guest writes to its standard output as soon as it is
// written:
//
//	host: writer goroutine
//	   |   "valuefromstdin0\n" ... "valuefromstdin9\n", one per second
//	   v
//	[stdin pipe]
//	   |
//	   v
//	guest: evaluator.wasm reads a line from os.Stdin and prints it to os.Stdout
//	   |   "From Stdin: valuefromstdin0\n"
//	   v
//	[stdout pipe]
//	   |
//	   v
//	host: reader goroutine prints "From stdout: From Stdin: valuefromstdin0"
//
// Why the guest needs the host for every byte of I/O: WebAssembly code can
// only compute, using numbers and its own private block of memory ("linear
// memory"). It cannot open a file, print text or read the clock on its own.
// For anything like that it calls functions that it imports from the host.
// WASI, the WebAssembly System Interface, standardizes a set of these
// functions: fd_read, fd_write, args_get, clock_time_get, proc_exit and more.
// Go's wasip1 port targets WASI "preview 1", whose functions are imported from
// a module named wasi_snapshot_preview1, and wazero implements all of them in
// Go. So when the guest calls fmt.Println, this happens:
//
//	guest: fmt.Println -> os.Stdout.Write -> WASI fd_write(fd 1, bytes)
//	host:  wazero's fd_write copies the bytes out of the guest's memory
//	       and writes them to the io.Writer the host configured as the
//	       guest's stdout (in this program, the stdout pipe)
package main

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// evaluatorWasm is the compiled guest, evaluator/evaluator.wasm.
//
// The //go:embed directive below copies the file's bytes into this variable
// when the host is compiled, so the host binary is self-contained and does
// not need to find the .wasm file at run time. The file has to exist at build
// time, which is why the compiled guest is committed to the repository.
//
// The //go:generate directive records how the .wasm file is made. After
// changing evaluator/evaluator.go, run `go generate` to rebuild it. go
// generate runs the command directly, without a shell, so `env` is what sets
// GOOS and GOARCH for that one build. The other two flags make the build
// reproducible, so rebuilding unchanged code gives a byte-identical file:
// -trimpath leaves the absolute paths of your source files and Go
// installation out of the binary, and -buildvcs=false leaves out the Git
// commit it was built from.
//
//go:generate env GOOS=wasip1 GOARCH=wasm go build -trimpath -buildvcs=false -o evaluator/evaluator.wasm ./evaluator
//go:embed evaluator/evaluator.wasm
var evaluatorWasm []byte

func main() {
	// Every wazero call takes a context.Context. Cancelling it can stop a
	// running guest, but only if the runtime was created with
	// wazero.NewRuntimeConfig().WithCloseOnContextDone(true), which is off by
	// default. This guest runs until its input ends, so a background context
	// is all we need.
	ctx := context.Background()

	// Step 1: create the runtime.
	//
	// A Runtime compiles and runs WebAssembly modules. On amd64 and arm64
	// (Linux, macOS, Windows, FreeBSD, NetBSD), wazero uses its optimizing
	// compiler, which turns the Wasm bytecode into native machine code before
	// running it. Elsewhere it falls back to an interpreter. Both are pure Go.
	r := wazero.NewRuntime(ctx)
	// Closing the runtime releases everything it created: compiled code,
	// module instances and their memory.
	defer r.Close(ctx)

	// Step 2: provide the host functions the guest imports.
	//
	// A Go wasip1 binary imports its system calls from a module named
	// "wasi_snapshot_preview1": fd_read, fd_write, args_get, proc_exit and so
	// on. This registers wazero's Go implementation of all of them under that
	// name. Without this step, Step 7 fails with the error
	// "module[wasi_snapshot_preview1] not instantiated".
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	// Step 3: compile the guest.
	//
	// Compiling checks that the bytes are a valid module and translates them
	// to machine code. It is the slow part: a few hundred milliseconds for
	// this 2.5 MB module. A compiled module can then be instantiated many
	// times, and each instance gets its own memory, so instances cannot see
	// each other's data. To skip compiling on every start, give the runtime a
	// cache on disk (see wazero.NewCompilationCacheWithDir).
	compiled, err := r.CompileModule(ctx, evaluatorWasm)
	if err != nil {
		log.Panicln(err)
	}

	run(ctx, r, compiled)
}

// run starts one instance of the compiled guest and streams data through it:
// lines go in through its stdin and come back out through its stdout while it
// is running.
func run(ctx context.Context, r wazero.Runtime, compiled wazero.CompiledModule) {
	// Step 4: create two pipes, one for each direction.
	//
	// io.Pipe returns a connected reader and writer with no buffer in between:
	// a Write blocks until Reads have taken all of its bytes, and a Read
	// blocks until there is something to read. Closing the writer makes the
	// reader return io.EOF; closing the reader makes later Writes fail with
	// io.ErrClosedPipe.
	//
	// Pipes let host and guest talk while the guest is running. With a
	// strings.Reader as stdin and a bytes.Buffer as stdout, the guest could
	// only read input that existed before it started, and the host would only
	// see the output after the guest had finished.
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	// Step 5: start one goroutine per pipe.
	//
	// Running the guest (Step 7) blocks the calling goroutine until the guest
	// exits, so feeding its stdin and reading its stdout must happen in other
	// goroutines, at the same time. They are not optional: without the
	// reader, the guest's first fmt.Println would block in fd_write forever,
	// because a pipe Write waits for a Read that would never come.

	// The writer sends ten lines to the guest's stdin, one per second.
	go func() {
		// Closing the write end is how the guest learns that the input is
		// over: its next read returns EOF, its scanner loop ends and its main
		// function returns.
		defer stdinWriter.Close()
		for i := range 10 {
			// The guest reads line by line, so each value ends with "\n".
			if _, err := fmt.Fprintf(stdinWriter, "valuefromstdin%d\n", i); err != nil {
				return // The guest stopped early and Step 8 closed the pipe.
			}
			// The pause makes the streaming visible: each line travels through
			// the guest and back out before the next one is sent.
			time.Sleep(time.Second)
		}
	}()

	// The reader prints every line the guest writes, prefixed so you can see
	// which output passed through the host. sync.WaitGroup.Go (Go 1.25+)
	// starts the goroutine and counts it, so Step 8 can wait for it.
	var wg sync.WaitGroup
	wg.Go(func() {
		scanner := bufio.NewScanner(stdoutReader)
		for scanner.Scan() {
			fmt.Println("From stdout:", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Println("Error reading from stdout:", err)
		}
	})

	// Step 6: decide what the guest is allowed to see.
	//
	// A guest gets nothing unless the host grants it: no arguments, no
	// environment variables, no files, a stdin that is always at EOF, a stdout
	// and stderr that are thrown away, and fake clocks and random numbers that
	// are the same on every run. This is what makes it safe to run code you
	// did not write, and it is why the guest's stdio has to be connected here
	// explicitly.
	//
	// A ModuleConfig never changes: every With... method returns a new copy.
	config := wazero.NewModuleConfig().
		// The guest sees these as os.Args. By convention os.Args[0] is the
		// program name, so the first real argument is os.Args[1].
		WithArgs("evaluator", "valuefromargs").
		// File descriptor 0: each read by the guest (WASI fd_read) calls
		// stdinReader.Read, which blocks until the writer goroutine sends a
		// line.
		WithStdin(stdinReader).
		// File descriptor 1: each write by the guest (WASI fd_write) goes to
		// stdoutWriter and comes out of stdoutReader in the reader goroutine.
		WithStdout(stdoutWriter).
		// File descriptor 2: error messages, including a panic in the guest,
		// go straight to the host's own stderr.
		WithStderr(os.Stderr)

	// Step 7: run the guest.
	//
	// InstantiateModule connects the guest's imports to the WASI functions
	// from Step 2, sets up its memory, and calls its "_start" export, WASI's
	// equivalent of main. For a Go guest, _start initializes the Go runtime,
	// runs main.main, and then calls the WASI function proc_exit with the exit
	// code. So this call returns only after the guest program has finished,
	// and by then the instance has already been closed.
	//
	// (Since Go 1.24, a guest built with -buildmode=c-shared can instead stay
	// alive and export functions marked //go:wasmexport for the host to call
	// whenever it likes. This example uses the classic run-to-completion
	// style.)
	_, err := r.InstantiateModule(ctx, compiled, config)

	// Step 8: clean up, then report how the guest ended.
	//
	// wazero never closes the readers and writers it was given, so that is our
	// job:
	//   - Closing stdinReader unblocks the writer goroutine if it is still
	//     trying to send, which only happens if the guest stopped before
	//     reading all of its input.
	//   - Closing stdoutWriter lets the reader goroutine reach EOF after it has
	//     printed everything the guest wrote.
	// wg.Wait then waits for that last line to be printed. Without it, main
	// could return, and the process exit, before it was printed.
	stdinReader.Close()
	stdoutWriter.Close()
	wg.Wait()

	// Exit code 0 is not an error: wazero returns nil. Any other exit code,
	// from os.Exit(n) in the guest, or 2 when the guest panics, comes back as
	// a *sys.ExitError. Anything else means the guest failed in a way it could
	// not report itself, such as a WebAssembly trap.
	if err != nil {
		// errors.AsType (Go 1.26+) is the generic form of errors.As: it finds a
		// *sys.ExitError even if another error wraps it.
		if exitErr, ok := errors.AsType[*sys.ExitError](err); ok {
			fmt.Fprintf(os.Stderr, "exit_code: %d\n", exitErr.ExitCode())
		} else {
			log.Panicln(err)
		}
	}
}
