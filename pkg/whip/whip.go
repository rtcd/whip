package whip

import (
	"log"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
)

type WHIPConn struct {
	pc      *webrtc.PeerConnection
	OnTrack func(pc *webrtc.PeerConnection, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
}

func NewWHIPConn() (*WHIPConn, error) {

	// Create a MediaEngine object to configure the supported codec
	m := &webrtc.MediaEngine{}

	m.RegisterDefaultCodecs()

	// Create a InterceptorRegistry. This is the user configurable RTP/RTCP Pipeline.
	// This provides NACKs, RTCP Reports and other features. If you use `webrtc.NewPeerConnection`
	// this is enabled by default. If you are manually managing You MUST create a InterceptorRegistry
	// for each PeerConnection.
	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}

	// Create the API object with the MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers:   []webrtc.ICEServer{},
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
	}
	// Create a new RTCPeerConnection
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	whip := &WHIPConn{
		pc: peerConnection,
	}

	// Accept one audio and one video track incoming
	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := peerConnection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			log.Print(err)
		}
	}

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Track has started, of type %d: %s \n", track.PayloadType(), track.Codec().MimeType)

		if whip.OnTrack != nil {
			go whip.OnTrack(peerConnection, track, receiver)
		}
	})

	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("Peer Connection State has changed: %s\n", s.String())
	})

	return whip, nil
}

func (w *WHIPConn) Offer(offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {

	// Set the remote SessionDescription
	err := w.pc.SetRemoteDescription(offer)
	if err != nil {
		log.Printf("SetRemoteDescription err %v ", err)
		w.pc.Close()
		return nil, err
	}

	// Create an answer
	answer, err := w.pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("CreateAnswer err %v ", err)
		w.pc.Close()
		return nil, err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(w.pc)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = w.pc.SetLocalDescription(answer); err != nil {
		log.Printf("SetLocalDescription err %v ", err)
		w.pc.Close()
		return nil, err
	}

	<-gatherComplete

	// Output the answer in base64 so we can paste it in browser
	return w.pc.LocalDescription(), nil
}

func (w *WHIPConn) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	return w.pc.AddICECandidate(candidate)
}

func (w *WHIPConn) Close() {
	if w.pc != nil && w.pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
		if cErr := w.pc.Close(); cErr != nil {
			log.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}
}
