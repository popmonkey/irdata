all: build/fetch-osx.zip build/fetch-win386.zip build/fetch-linux386.zip

.PHONY: clean
clean:
	rm build/*

build/fetch.osx-arm64:
	cd examples/fetch && \
	GOOS=darwin GOARCH=arm64 go build -o ../../build/fetch.osx-arm64

build/fetch.osx-amd64:
	cd examples/fetch && \
	GOOS=darwin GOARCH=amd64 go build -o ../../build/fetch.osx-amd64

build/fetch-osx.zip: build/fetch.osx-arm64 build/fetch.osx-amd64
	lipo -create -output build/fetch.osx-universal build/fetch.osx-arm64 build/fetch.osx-amd64
	cd build && \
	zip fetch-osx.zip fetch.osx-universal

build/fetch-win386.zip:
	cd examples/fetch && \
	GOOS=windows GOARCH=386 go build -o ../../build/fetch.exe
	cd build && \
	zip fetch-win386.zip fetch.exe

build/fetch-linux386.zip:
	cd examples/fetch && \
	GOOS=linux GOARCH=386 go build -o ../../build/fetch
	cd build && \
	zip fetch-linux386.zip fetch
