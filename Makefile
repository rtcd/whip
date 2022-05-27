all:
	export PKG_CONFIG_PATH=/usr/local/lib/pkgconfig
	go build -o bin/webrtc2rtmp examples/webrtc2rtmp/main.go
	go build -o bin/one2many examples/one2many/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags "-s -w" -o bin/one2many-arm examples/one2many/main.go
