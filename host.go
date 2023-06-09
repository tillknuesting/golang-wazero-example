package main

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// addWasm was generated by the following:
//
//	cd evaluator; GOARCH=wasm GOOS=wasip1 gotip build -o evaluator.wasm evaluator.go; cd ..
//
//go:embed evaluator/evaluator.wasm
var evaluatorWasm []byte

func main() {
	// Choose the context to use for function calls.
	ctx := context.Background()

	// Create a new WebAssembly Runtime.
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx) // This closes everything this Runtime created.

	// Combine the above into our baseline config, overriding defaults.
	config := wazero.NewModuleConfig().
		// By default, I/O streams are discarded and there's no file system.
		WithStdout(os.Stdout).WithStderr(os.Stderr)

	// Instantiate WASI, which implements system I/O such as console output.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	run(r, ctx, config)
}

func run(r wazero.Runtime, ctx context.Context, config wazero.ModuleConfig) {
	// Create an io.Pipe
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	// Write to stdinWriter in a separate goroutine
	go func() {
		defer stdinWriter.Close()
		for i := 0; i < 10; i++ {
			stdinWriter.Write([]byte(fmt.Sprintf("valuefromstdin%d\n", i)))
			time.Sleep(1 * time.Second)
		}
	}()

	// Read from stdoutReader in a separate goroutine
	go func() {
		reader := bufio.NewReader(stdoutReader)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				} else {
					log.Println("Error reading from stdout:", err)
				}
			}
			fmt.Println("From stdout: ", line)
		}
	}()

	// InstantiateModule runs the "_start" function, WASI's "main".
	_, err := r.InstantiateWithConfig(ctx, evaluatorWasm, config.WithArgs("wasi",
		"valuefromargs").WithStdout(stdoutWriter).WithStdin(stdinReader))
	if err != nil {
		// Note: Most compilers do not exit the module after running "_start",
		// unless there was an error. This allows you to call exported functions.
		if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
			fmt.Fprintf(os.Stderr, "exit_code: %d\n", exitErr.ExitCode())
		} else if !ok {
			log.Panicln(err)
		}
	}
}
