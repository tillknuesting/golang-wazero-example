# golang-wazero-example

As Go 1.21 is not released yet - please use gotip to install the main branch version:
Install Prerequisites:
```bash
go install http://golang.org/dl/gotip@latest
gotip download
brew install wazero
```

Build the Wasm module (called evaluator):
```bash
cd evaluator; GOARCH=wasm GOOS=wasip1 gotip build -o evaluator.wasm evaluator.go; cd ..
```

Run:
```bash
gotip run host.go
```
