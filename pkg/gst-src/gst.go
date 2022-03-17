// Package gst provides an easy API to create an appsink pipeline
package gst

/*
#cgo pkg-config: gstreamer-1.0 gstreamer-app-1.0

#include "gst.h"

*/
import "C"
import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	idIdx int = 0
)

func init() {
	go C.gstreamer_send_start_mainloop()
}

// Pipeline is a wrapper for a GStreamer Pipeline
type Pipeline struct {
	Pipeline *C.GstElement
	tracks   []*webrtc.TrackLocalStaticSample
	id       int
	//codecName string
	//clockRate float32
}

var pipelines = make(map[int]*Pipeline)
var pipelinesLock sync.Mutex

const (
	videoClockRate = 90000
	audioClockRate = 48000
	pcmClockRate   = 8000
)

// CreatePipeline creates a GStreamer Pipeline
func CreatePipeline( /*codecName string, */ tracks []*webrtc.TrackLocalStaticSample, srcUrl string) *Pipeline {
	var pipelineStr string

	videoPipelineStr := " ! vp8enc target-bitrate=1500 error-resilient=partitions keyframe-max-dist=15 auto-alt-ref=true cpu-used=5 deadline=1 ! "
	audioPipelineStr := " ! audioresample ! audio/x-raw,rate=48000 ! opusenc ! "

	if strings.HasPrefix(srcUrl, "rtmp://") {
		pipelineStr = "rtmpsrc location=" + srcUrl + " ! flvdemux name=s_0"
		pipelineStr += " s_0.audio ! queue ! decodebin ! audioconvert " + audioPipelineStr + " appsink name=audio"
		pipelineStr += " s_0.video ! queue ! decodebin ! videoconvert " + videoPipelineStr + " appsink name=video"
	} else if strings.HasPrefix(srcUrl, "rtsp://") {
		pipelineStr = "rtspsrc location=" + srcUrl + " protocols=tcp latency=300 name=s_0"
		pipelineStr += " s_0. ! application/x-rtp,media=audio ! rtpjitterbuffer ! decodebin ! audioconvert " + audioPipelineStr + " appsink name=audio"
		pipelineStr += " s_0. ! application/x-rtp,media=video ! rtpjitterbuffer ! decodebin ! videoconvert " + videoPipelineStr + " appsink name=video"
	}

	fmt.Printf("Pipeline: %s\n", pipelineStr)

	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	pipelinesLock.Lock()
	defer pipelinesLock.Unlock()

	pipeline := &Pipeline{
		Pipeline: C.gstreamer_send_create_pipeline(pipelineStrUnsafe),
		tracks:   tracks,
		id:       idIdx,
	}
	idIdx++
	pipelines[pipeline.id] = pipeline
	return pipeline
}

// Start starts the GStreamer Pipeline
func (p *Pipeline) Start() {
	C.gstreamer_send_start_pipeline(p.Pipeline, C.int(p.id))
}

// Stop stops the GStreamer Pipeline
func (p *Pipeline) Stop() {
	C.gstreamer_send_stop_pipeline(p.Pipeline)
}

//export goHandlePipelineBuffer
func goHandlePipelineBuffer(buffer unsafe.Pointer, bufferLen C.int, duration C.int, pipelineID C.int, trackIdx C.int) {
	pipelinesLock.Lock()
	pipeline, ok := pipelines[int(pipelineID)]
	pipelinesLock.Unlock()

	go func() {
		if ok {
			if t := pipeline.tracks[int(trackIdx)]; t != nil {
				if err := t.WriteSample(media.Sample{Data: C.GoBytes(buffer, bufferLen), Duration: time.Duration(duration)}); err != nil {
					panic(err)
				}
			}
		} else {
			fmt.Printf("discarding buffer, no pipeline with id %d", int(pipelineID))
		}
		C.free(buffer)
	}()
}
