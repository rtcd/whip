// Package gst provides an easy API to create an appsrc pipeline
package gst

/*
#cgo pkg-config: gstreamer-1.0 gstreamer-app-1.0

#include "gst.h"

*/
import "C"
import (
	"unsafe"
)

// StartMainLoop starts GLib's main loop
// It needs to be called from the process' main thread
// Because many gstreamer plugins require access to the main thread
// See: https://golang.org/pkg/runtime/#LockOSThread
func StartMainLoop() {
	C.gstreamer_receive_start_mainloop()
}

// Pipeline is a wrapper for a GStreamer Pipeline
type Pipeline struct {
	Pipeline *C.GstElement
}

// CreatePipeline creates a GStreamer Pipeline
func CreatePipeline(rtmpUrl string) *Pipeline {
	publish := "flvmux name=mux streamable=true ! rtmp2sink sync=false location=" + rtmpUrl
	pVStr := " appsrc format=time is-live=1 do-timestamp=true name=video ! queue ! application/x-rtp, encoding-name=VP8-DRAFT-IETF-01 ! rtpvp8depay ! decodebin ! videoscale ! video/x-raw,width=1280,height=720 ! x264enc bitrate=1000 tune=zerolatency key-int-max=90 ! video/x-h264 ! h264parse ! video/x-h264 ! mux. "
	pAStr := " appsrc format=time is-live=1 do-timestamp=true name=audio ! queue ! application/x-rtp, payload=96, encoding-name=OPUS ! rtpopusdepay ! decodebin ! audioresample ! audio/x-raw,rate=48000 ! faac bitrate=96000 ! audio/mpeg ! aacparse ! audio/mpeg, mpegversion=4 ! mux."
	pipelineStr := publish + pVStr + pAStr
	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))
	return &Pipeline{Pipeline: C.gstreamer_receive_create_pipeline(pipelineStrUnsafe)}
}

// Start starts the GStreamer Pipeline
func (p *Pipeline) Start() {
	C.gstreamer_receive_start_pipeline(p.Pipeline)
}

// Stop stops the GStreamer Pipeline
func (p *Pipeline) Stop() {
	C.gstreamer_receive_stop_pipeline(p.Pipeline)
}

// Push pushes a buffer on the appsrc of the GStreamer Pipeline
func (p *Pipeline) Push(buffer []byte, src string) {
	b := C.CBytes(buffer)
	defer C.free(b)

	strUnsafe := C.CString(src)
	defer C.free(unsafe.Pointer(strUnsafe))
	C.gstreamer_receive_push_buffer(p.Pipeline, b, C.int(len(buffer)), strUnsafe)
}
