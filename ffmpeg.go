package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
)

type FFmpegStreamer struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewFFmpegStreamer creates a new FFmpeg streamer instance
func NewFFmpegStreamer() *FFmpegStreamer {
	return &FFmpegStreamer{}
}

// StreamVideoFile streams a video file to a virtual camera
func (f *FFmpegStreamer) StreamVideoFile(inputFile, outputDevice string) error {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel

	args := []string{
		"-re",
		"-i", inputFile,
		"-vcodec", "rawvideo",
		"-pix_fmt", "yuyv422",
		"-video_size", "640x480",
		"-f", "video4linux2",
		outputDevice,
	}

	return f.startFFmpeg(ctx, args)
}

// StreamWebcam streams from one webcam to a virtual camera
func (f *FFmpegStreamer) StreamWebcam(inputDevice, outputDevice string) error {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel

	args := []string{
		"-f", "v4l2",
		"-framerate", "30",
		"-video_size", "640x480",
		"-i", inputDevice,
		"-vcodec", "rawvideo",
		"-pix_fmt", "yuyv422",
		"-video_size", "640x480",
		"-f", "video4linux2",
		outputDevice,
	}

	return f.startFFmpeg(ctx, args)
}

// StreamScreen captures and streams the screen to a virtual camera
func (f *FFmpegStreamer) StreamScreen(display, resolution, outputDevice string) error {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel

	args := []string{
		"-f", "x11grab",
		"-framerate", "30",
		"-video_size", resolution,
		"-i", display,
		"-vcodec", "rawvideo",
		"-pix_fmt", "yuyv422",
		"-video_size", "640x480",
		"-f", "video4linux2",
		outputDevice,
	}

	return f.startFFmpeg(ctx, args)
}

// Stop stops the current streaming process
func (f *FFmpegStreamer) Stop() {
	if f.cancel != nil {
		// Signal process to stop
		f.cancel()
		// Wait for process to finish
		f.wg.Wait()
		// Reset cancel function
		f.cancel = nil
		// Kill the process if it's still running
		if f.cmd != nil && f.cmd.Process != nil {
			f.cmd.Process.Kill()
			f.cmd = nil
		}
	}
}

// startFFmpeg starts the FFmpeg process with the given arguments
func (f *FFmpegStreamer) startFFmpeg(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	
	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	f.cmd = cmd
	f.wg.Add(2)

	// Handle stdout in a goroutine
	go func() {
		defer f.wg.Done()
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("Error reading stdout: %v", err)
				return
			}
			log.Printf("FFmpeg: %s", line)
		}
	}()

	// Handle stderr in a goroutine
	go func() {
		defer f.wg.Done()
		reader := bufio.NewReader(stderr)
		for {
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("Error reading stderr: %v", err)
				return
			}
			// Only log actual errors, not info messages
			if strings.Contains(line, "Error") || 
			   strings.Contains(line, "error") || 
			   strings.Contains(line, "failed") || 
			   strings.Contains(line, "Failed") || 
			   strings.Contains(line, "Invalid") || 
			   strings.Contains(line, "invalid") {
				log.Printf("FFmpeg Error: %s", line)
			}
		}
	}()

	// Wait for the command to complete in a goroutine
	go func() {
		err := cmd.Wait()
		if err != nil && ctx.Err() == nil {
			log.Printf("FFmpeg process ended with error: %v", err)
		}
	}()

	return nil
}


