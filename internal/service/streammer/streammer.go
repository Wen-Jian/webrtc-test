package streammer

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"
)

const (
	width  = 494
	height = 370
)

type Streammer struct {
	Data chan []byte

	lock sync.Mutex
}

func NewStreamer(receiver chan []byte) *Streammer {
	return &Streammer{
		Data: receiver,
		lock: sync.Mutex{},
	}
}

func (str *Streammer) StartGstreamer() {
	gstPipeline := exec.Command(
		"gst-launch-1.0",
		"--gst-debug-level=3", // Debug logs
		"fdsrc", "fd=0", "!",  // Read from stdin
		"rtpjitterbuffer", "!", // Ensure timestamps are set
		"rtpvp8depay", "!", // Depayload RTP packets
		"vp8dec", "!", // Decode VP8
		"videoconvert", "!", // Convert format
		"video/x-raw, format=I420", "!", // Specify YUV420p (I420 is equivalent)
		"v4l2sink", "device=/dev/video2", // Send to virtual camera
	)

	// Get stdin pipe
	stdin, err := gstPipeline.StdinPipe()
	if err != nil {
		fmt.Println("Error getting StdinPipe:", err)
		return
	}

	// Start GStreamer pipeline
	err = gstPipeline.Start()
	if err != nil {
		fmt.Println("Error starting GStreamer pipeline:", err)
		return
	}

	// go func() {
	// 	output, err := gstPipeline.CombinedOutput()
	// 	if err != nil {
	// 		fmt.Println("Error starting GStreamer:", err)
	// 		fmt.Println("GStreamer output:", string(output))
	// 		return
	// 	}
	// }()

	for {
		str.lock.Lock()
		var frame []byte
		select {
		case frame = <-str.Data:
			// default:
			// 	frame = str.generateRedFrame()
		}
		str.lock.Unlock()

		log.Println("receiving data")

		_, err = stdin.Write(frame)
		if err != nil {
			fmt.Println("Error writing to GStreamer stdin:", err)

			break
		}

		time.Sleep(33 * time.Millisecond) // ~30 FPS
	}
}

func (str *Streammer) StartFfmpeg() {
	cmd := exec.CommandContext(context.Background(),
		"ffmpeg", "-loglevel", "verbose", "-f", "rawvideo", "-pix_fmt", "yuv420p",
		"-s", fmt.Sprintf("%dx%d", width, height), "-r", "30", "-i", "-",
		"-f", "v4l2", "/dev/video2")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println("Error opening stdin pipe:", err)
		return
	}

	err = cmd.Start()
	if err != nil {
		fmt.Println("Error starting ffmpeg:", err)
		return
	}

	for {
		str.lock.Lock()
		var frame []byte
		select {
		case frame = <-str.Data:
		default:
			frame = str.generateRedFrame()
		}
		str.lock.Unlock()

		_, err := stdin.Write(frame)
		if err != nil {
			fmt.Println("Error writing to ffmpeg stdin:", err)
			break
		}

		time.Sleep(33 * time.Millisecond) // ~30 FPS
	}

	stdin.Close()
	cmd.Wait()
}

func (str *Streammer) generateRedFrame() []byte {
	frameSize := (width * height * 3) / 2
	YSize := width * height
	UVSize := YSize / 4
	frame := make([]byte, frameSize)

	// Set Y plane (brightness)
	for i := 0; i < YSize; i++ {
		frame[i] = 76 // Approximate for red
	}
	// Set U and V planes
	for i := YSize; i < YSize+UVSize; i++ {
		frame[i] = 85 // Approximate for red
	}
	for i := YSize + UVSize; i < frameSize; i++ {
		frame[i] = 255 // Strong red tint
	}
	return frame
}
