package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kataras/iris/v12"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/rtcd/whip/pkg/whip"
)

var (
	addr    = "localhost:8080"
	cert    = ""
	key     = ""
	webRoot = "html"

	listLock sync.RWMutex
	conns    = make(map[string]*whipState)
)

type whipState struct {
	whipConn *whip.WHIPConn
	oggFile  *oggwriter.OggWriter
	ivfFile  *ivfwriter.IVFWriter
}

func newWhipState(id string, whip *whip.WHIPConn) *whipState {

	oggFile, err := oggwriter.New(id+"_output.ogg", 48000, 2)
	if err != nil {
		log.Print(err)
		return nil
	}
	ivfFile, err := ivfwriter.New(id + "_output.ivf")
	if err != nil {
		log.Print(err)
		return nil
	}

	return &whipState{
		whipConn: whip,
		oggFile:  oggFile,
		ivfFile:  ivfFile,
	}
}

func saveToDisk(i media.Writer, track *webrtc.TrackRemote) {
	defer func() {
		if err := i.Close(); err != nil {
			log.Print(err)
		}
	}()

	for {
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			log.Print(err)
			return
		}
		if err := i.WriteRTP(rtpPacket); err != nil {
			log.Print(err)
			return
		}
	}
}

func showHelp() {
	fmt.Printf("Usage:%s {params}\n", os.Args[0])
	fmt.Println("      -cert {cert file for https}")
	fmt.Println("      -key {key file for https}")
	fmt.Println("      -bind {bind listen addr}")
	fmt.Println("      -web {html root directory}")
	fmt.Println("      -h (show help info)")
}

func main() {
	flag.StringVar(&cert, "cert", "", "cert file")
	flag.StringVar(&key, "key", "", "key file")
	flag.StringVar(&addr, "addr", "localhost:8080", "http listening address")
	flag.StringVar(&webRoot, "web", "html", "html root directory")
	help := flag.Bool("h", false, "help info")
	flag.Parse()

	if *help {
		showHelp()
		return
	}

	app := iris.New()
	app.Logger().SetLevel("debug")

	app.HandleDir("/", "./"+webRoot, iris.DirOptions{
		IndexName: "/index.html",
		Gzip:      true,
		ShowList:  false,
	})

	app.Post("/whip/{room}/{stream}", func(ctx iris.Context) {
		roomId := ctx.Params().Get("room")
		streamId := ctx.Params().Get("stream")
		body, _ := ctx.GetBody()
		log.Printf("Post: roomId => %v, streamId => %v, body = %v", roomId, streamId, string(body))
		listLock.Lock()
		defer listLock.Unlock()
		if _, found := conns[streamId]; !found {
			whip, err := whip.NewWHIPConn()
			if err != nil {
				ctx.WriteString("failed to create whip conn!")
				return
			}

			state := newWhipState(streamId, whip)

			whip.OnTrack = func(pc *webrtc.PeerConnection, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {

				if track.Kind() == webrtc.RTPCodecTypeVideo {
					// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
					// This is a temporary fix until we implement incoming RTCP events, then we would push a PLI only when a viewer requests it
					go func() {
						ticker := time.NewTicker(time.Second * 3)
						for range ticker.C {
							errSend := pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
							if errSend != nil {
								log.Println(errSend)
								return
							}
						}
					}()
				}

				codec := track.Codec()
				if strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) {
					fmt.Println("Got Opus track, saving to disk as " + streamId + "_output.opus (48 kHz, 2 channels)")
					saveToDisk(state.oggFile, track)
				} else if strings.EqualFold(codec.MimeType, webrtc.MimeTypeVP8) {
					fmt.Println("Got VP8 track, saving to disk as " + streamId + "_output.ivf")
					saveToDisk(state.ivfFile, track)
				}

				/*
					buf := make([]byte, 1500)
					for {
						_, _, err := track.Read(buf)
						if err != nil {
							return
						}
						// TODO: recv rtp from track.

						if _, err = trackLocal.Write(buf[:i]); err != nil {
							return
						}
					}
				*/

			}

			conns[streamId] = state
			answer, _ := whip.Offer(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: string(body)})
			log.Printf("post: answer => %v", answer.SDP)
			ctx.ContentType("application/sdp")
			ctx.WriteString(answer.SDP)
		} else {
			ctx.WriteString("stream " + streamId + " already exists")
		}
	})

	app.Patch("/whip/{room}/{stream}", func(ctx iris.Context) {
		roomId := ctx.Params().Get("room")
		streamId := ctx.Params().Get("stream")
		body, _ := ctx.GetBody()
		log.Printf("Patch: roomId => %v, streamId => %v, body = %v", roomId, streamId, string(body))
		listLock.Lock()
		defer listLock.Unlock()
		if state, found := conns[streamId]; found {
			mid := "0"
			index := uint16(0)
			state.whipConn.AddICECandidate(webrtc.ICECandidateInit{Candidate: string(body), SDPMid: &mid, SDPMLineIndex: &index})
			ctx.ContentType("application/trickle-ice-sdpfrag")
			ctx.WriteString("")
		}
	})

	app.Delete("/whip/{room}/{stream}", func(ctx iris.Context) {
		roomId := ctx.Params().Get("room")
		streamId := ctx.Params().Get("stream")
		body, _ := ctx.GetBody()
		log.Printf("Delete: roomId => %v, streamId => %v, body = %v", roomId, streamId, string(body))

		listLock.Lock()
		defer listLock.Unlock()
		if state, found := conns[streamId]; found {
			state.whipConn.Close()
			delete(conns, streamId)
		}
		ctx.WriteString("")
	})

	if cert != "" && key != "" {
		app.Run(iris.TLS(addr, cert, key), iris.WithoutServerError(iris.ErrServerClosed))
	} else {
		app.Run(iris.Addr(addr), iris.WithoutServerError(iris.ErrServerClosed))
	}
}
