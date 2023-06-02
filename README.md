# golang-wazero-example

Install Prerequisites:
```bash
go install http://golang.org/dl/gotip@latest
gotip download
brew install wazero
```

Build:
```bash
cd evaluator; GOARCH=wasm GOOS=wasip1 gotip build -o evaluator.wasm evaluator.go; cd ..
```

Run:
```bash
gotip run host.go
```