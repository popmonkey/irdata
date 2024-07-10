all: build/irfetch-osx.zip build/irfetch-win386.zip build/irfetch-linux386.zip

.PHONY: clean
clean:
	rm build/*

build/irfetch.osx-arm64: *.go examples/irfetch/*.go
	cd examples/irfetch && \
	GOOS=darwin GOARCH=arm64 go build -o ../../build/irfetch.osx-arm64

build/irfetch.osx-amd64: *.go examples/irfetch/*.go
	cd examples/irfetch && \
	GOOS=darwin GOARCH=amd64 go build -o ../../build/irfetch.osx-amd64

build/irfetch-osx.zip: build/irfetch.osx-arm64 build/irfetch.osx-amd64
	lipo -create -output build/irfetch.osx-universal build/irfetch.osx-arm64 build/irfetch.osx-amd64
	cd build && \
	zip irfetch-osx.zip irfetch.osx-universal

build/irfetch-win386.zip: *.go examples/irfetch/*.go
	cd examples/irfetch && \
	GOOS=windows GOARCH=386 go build -o ../../build/irfetch.exe
	cd build && \
	zip irfetch-win386.zip irfetch.exe

build/irfetch-linux386.zip: *.go examples/irfetch/*.go
	cd examples/irfetch && \
	GOOS=linux GOARCH=386 go build -o ../../build/irfetch
	cd build && \
	zip irfetch-linux386.zip irfetch
