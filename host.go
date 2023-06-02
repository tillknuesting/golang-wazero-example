package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
	"log"
	"os"
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
	var buf bytes.Buffer

	// InstantiateModule runs the "_start" function, WASI's "main".
	if _, err := r.InstantiateWithConfig(ctx, evaluatorWasm, config.WithArgs("wasi", "testvalue").WithStdout(&buf)); err != nil {
		// Note: Most compilers do not exit the module after running "_start",
		// unless there was an error. This allows you to call exported functions.
		if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
			fmt.Fprintf(os.Stderr, "exit_code: %d\n", exitErr.ExitCode())
		} else if !ok {
			log.Panicln(err)
		}
	}

	fmt.Println(buf.String())
}
