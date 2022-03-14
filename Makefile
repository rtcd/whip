all:
	PKG_CONFIG_PATH=/usr/local/lib/pkgconfig && go build -o whip .