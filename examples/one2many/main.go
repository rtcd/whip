package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mdp/qrterminal/v3"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"github.com/rtcd/whip/pkg/whip"
	"github.com/spf13/viper"
)

// Config defines parameters for configuring the sfu instance
type Config struct {
	whip.Config `mapstructure:",squash"`
}

var (
	conf     Config
	file     = ""
	addr     = ":8080"
	cert     = ""
	key      = ""
	webRoot  = "html"
	listLock sync.RWMutex
	conns    = make(map[string]*whipState)
)

// Add to list of tracks and fire renegotation for all PeerConnections
func addTrack(w *whipState, t *webrtc.TrackRemote) *webrtc.TrackLocalStaticRTP {
	listLock.Lock()
	defer func() {
		listLock.Unlock()
	}()

	// Create a new TrackLocal with the same codec as our incoming
	trackLocal, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
	if err != nil {
		panic(err)
	}

	w.trackLocals[t.ID()] = trackLocal
	return trackLocal
}

// Remove from list of tracks and fire renegotation for all PeerConnections
func removeTrack(w *whipState, t *webrtc.TrackLocalStaticRTP) {
	listLock.Lock()
	defer func() {
		listLock.Unlock()
	}()

	delete(w.trackLocals, t.ID())
}

type whipState struct {
	stream      string
	room        string
	publish     bool
	whipConn    *whip.WHIPConn
	trackLocals map[string]*webrtc.TrackLocalStaticRTP
}

func printQR(url string) {
	config := qrterminal.Config{
		Level:     qrterminal.L,
		Writer:    os.Stdout,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 1,
	}
	fmt.Println("WHIP publish QR Code:")
	qrterminal.GenerateWithConfig(url, config)
}

func getClientIp() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", errors.New("Can not find the client ip address!")
}

func showHelp() {
	fmt.Printf("Usage:%s {params}\n", os.Args[0])
	fmt.Println("      -c {config file}")
	fmt.Println("      -cert {cert file for https}")
	fmt.Println("      -key {key file for https}")
	fmt.Println("      -bind {bind listen addr}")
	fmt.Println("      -web {html root directory}")
	fmt.Println("      -h (show help info)")
}

func load(file string) bool {
	_, err := os.Stat(file)
	if err != nil {
		return false
	}

	viper.SetConfigFile(file)
	viper.SetConfigType("toml")

	err = viper.ReadInConfig()
	if err != nil {
		log.Print("config file read failed ", err, " file", file)
		return false
	}
	err = viper.GetViper().Unmarshal(&conf)
	if err != nil {
		log.Print("sfu config file loaded failed ", err, " file", file)
		return false
	}
	return true
}

func printWhipState() {
	log.Printf("State for whip:")
	for key, conn := range conns {
		streamType := "\tpublisher"
		if !conn.publish {
			streamType = "\tsubscriber"
		}
		log.Printf("%v: room: %v, stream: %v, resourceId: [%v]", streamType, conn.room, conn.stream, key)
	}
}

func main() {
	flag.StringVar(&file, "c", "config.toml", "config file")
	flag.StringVar(&cert, "cert", "", "cert file")
	flag.StringVar(&key, "key", "", "key file")
	flag.StringVar(&addr, "addr", ":8080", "http listening address")
	flag.StringVar(&webRoot, "web", "html", "html root directory")
	help := flag.Bool("h", false, "help info")
	flag.Parse()

	if !load(file) {
		return
	}

	if *help {
		showHelp()
		return
	}

	whip.Init(conf.Config)

	r := mux.NewRouter()

	r.HandleFunc("/whip/{room}/{stream}/{mode}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		roomId := vars["room"]
		streamId := vars["stream"]
		mode := vars["mode"]
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		log.Printf("Post: roomId => %v, streamId => %v, body = %v", roomId, streamId, string(body))

		listLock.Lock()
		defer listLock.Unlock()

		whip, err := whip.NewWHIPConn()

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			msg := "500 - failed to create whip conn!"
			log.Printf("%v", msg)
			w.Write([]byte(msg))
			return
		}

		if mode == "publish" {
			for _, wc := range conns {
				if wc.publish && wc.stream == streamId {
					w.WriteHeader(http.StatusInternalServerError)
					msg := "500 - publish conn [" + streamId + "] already exist!"
					log.Printf("%v", msg)
					w.Write([]byte(msg))
					return
				}
			}
		}

		state := &whipState{
			stream:      streamId,
			room:        roomId,
			publish:     mode == "publish",
			whipConn:    whip,
			trackLocals: make(map[string]*webrtc.TrackLocalStaticRTP),
		}

		if mode == "publish" {
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

				trackLocal := addTrack(state, track)
				defer removeTrack(state, trackLocal)

				buf := make([]byte, 1500)
				for {
					i, _, err := track.Read(buf)
					if err != nil {
						return
					}

					if _, err = trackLocal.Write(buf[:i]); err != nil {
						return
					}
				}
			}
		}

		if mode == "subscribe" {
			foundPublish := false
			for _, wc := range conns {
				if wc.publish && wc.stream == streamId {
					for trackID := range wc.trackLocals {
						if _, err := whip.AddTrack(wc.trackLocals[trackID]); err != nil {
							return
						}
					}
					go func() {
						time.Sleep(time.Second * 1)
						wc.whipConn.PictureLossIndication()
					}()
					foundPublish = true
				}
			}
			if !foundPublish {
				w.WriteHeader(http.StatusInternalServerError)
				msg := fmt.Sprintf("Not find any publisher for room: %v, stream: %v", roomId, streamId)
				log.Print(msg)
				w.Write([]byte(msg))
				return
			}
		}

		uniqueStreamId := mode + "-" + streamId + "-" + RandomString(12)

		conns[uniqueStreamId] = state

		log.Printf("got offer => %v", string(body))
		answer, err := whip.Offer(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: string(body)})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			msg := fmt.Sprintf("failed to answer whip conn: %v", err)
			log.Print(msg)
			w.Write([]byte(msg))
			return
		}
		log.Printf("send answer => %v", answer.SDP)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Location", "/whip/"+roomId+"/"+uniqueStreamId)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(answer.SDP))
		printWhipState()
	}).Methods("POST")

	r.HandleFunc("/whip/{room}/{stream}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		roomId := vars["room"]
		streamId := vars["stream"]
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		log.Printf("Patch: roomId => %v, streamId => %v, body = %v", roomId, streamId, string(body))
		listLock.Lock()
		defer listLock.Unlock()
		if state, found := conns[streamId]; found {
			mid := "0"
			index := uint16(0)
			state.whipConn.AddICECandidate(webrtc.ICECandidateInit{Candidate: string(body), SDPMid: &mid, SDPMLineIndex: &index})
			w.Header().Set("Content-Type", "application/trickle-ice-sdpfrag")
			w.WriteHeader(http.StatusCreated)
		}
	}).Methods("PATCH")

	r.HandleFunc("/whip/{room}/{stream}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		roomId := vars["room"]
		streamId := vars["stream"]

		log.Printf("Delete: roomId => %v, streamId => %v", roomId, streamId)

		listLock.Lock()
		defer listLock.Unlock()
		if state, found := conns[streamId]; found {
			state.whipConn.Close()
			delete(conns, streamId)
			streamType := "publish"
			if !state.publish {
				streamType = "subscribe"
			}
			log.Printf("%v stream conn removed  %v", streamType, streamId)
			printWhipState()
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			msg := "stream " + streamId + " not found"
			log.Print(msg)
			w.Write([]byte(msg))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(streamId + " deleted"))
	}).Methods("DELETE")

	r.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir(webRoot))))
	r.Headers("Access-Control-Allow-Origin", "*")

	if cert != "" && key != "" {
		if e := http.ListenAndServeTLS(addr, cert, key, r); e != nil {
			log.Fatal("ListenAndServeTLS: ", e)
		}
	} else {
		if e := http.ListenAndServe(addr, r); e != nil {
			log.Fatal("ListenAndServe: ", e)
		}
	}
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")

func RandomString(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
