all:
	export PKG_CONFIG_PATH=/usr/local/lib/pkgconfig
	go build -o bin/webrtc2rtmp examples/webrtc2rtmp/main.go
	go build -o bin/one2many examples/one2many/main.go

win:
	GOOS=windows GOARCH=386 CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/one2many-windows-i386.exe examples/one2many/main.go
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/one2many-windows-amd64.exe examples/one2many/main.go

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o bin/one2many-linux-amd64 examples/one2many/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags "-s -w" -o bin/one2many-linux-arm examples/one2many/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o bin/one2many-linux-arm64 examples/one2many/main.go