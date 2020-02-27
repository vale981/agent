build-macos64:
	mkdir -p ./bin/indihub-agent-macos64
	GOOS=darwin GOARCH=amd64 go build -v -o ./bin/indihub-agent-macos64/indihub-agent ./

build-linux64:
	mkdir -p ./bin/indihub-agent-linux64
	GOOS=linux GOARCH=amd64 go build -v -o ./bin/indihub-agent-linux64/indihub-agent ./

build-unix64:
	mkdir -p ./bin/indihub-agent-unix64
	GOOS=freebsd GOARCH=amd64 go build -v -o ./bin/indihub-agent-unix64/indihub-agent ./

build-win64:
	mkdir -p ./bin/indihub-agent-win64
	GOOS=windows GOARCH=amd64 go build -v -o ./bin/indihub-agent-win64/indihub-agent.exe ./

build-win32:
	mkdir -p ./bin/indihub-agent-win32
	GOOS=windows GOARCH=386 go build -v -o ./bin/indihub-agent-win32/indihub-agent.exe ./

build-raspberrypi:
	mkdir -p ./bin/indihub-agent-raspberrypi
	GOOS=linux GOARCH=arm GOARM=5 go build -v -o ./bin/indihub-agent-raspberrypi/indihub-agent ./

build-all: build-macos64 build-linux64 build-unix64 build-win64 build-win32 build-raspberrypi
