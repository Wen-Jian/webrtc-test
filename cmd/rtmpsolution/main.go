package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
	"webrtc-server/internal/ffmpeg"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for demo purposes
		},
	}

	// Virtual camera configuration
	VirtualCameraDevice = "/dev/video10"
	DefaultWebcamDevice = "/dev/video10"
)

type Message struct {
	Event string         `json:"event"`
	Data  map[string]any `json:"data"`
}

type Client struct {
	conn     *websocket.Conn
	peerConn *webrtc.PeerConnection
	mu       sync.Mutex
	streamer *ffmpeg.FFmpegStreamer
}

func (c *Client) sendJSON(msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WriteJSON(msg); err != nil {
		log.Printf("Error sending message: %v", err)
		return err
	}
	return nil
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Set aggressive cache prevention headers
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Add a timestamp query parameter to force reload
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))

	http.ServeFile(w, r, "static/index.html")
}

func main() {
	// Handle root path specially for aggressive cache prevention
	http.HandleFunc("/", serveIndex)
	// Serve other static files
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// WebSocket endpoint
	http.HandleFunc("/ws", handleWebSocket)

	log.Println("Server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Create client instance
	client := &Client{
		conn:     conn,
		streamer: ffmpeg.NewFFmpegStreamer(),
	}

	// Ensure cleanup on disconnect
	defer func() {
		conn.Close()
		// Stop any running stream
		if client.streamer != nil {
			client.streamer.Stop()
			time.Sleep(time.Second) // Give it time to clean up
		}
	}()

	// Configure WebRTC
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("Failed to create peer connection: %v", err)
		return
	}
	client.peerConn = peerConnection
	defer func() {
		peerConnection.Close()
		if client.streamer != nil {
			client.streamer.Stop()
		}
	}()

	// Set up handlers for WebRTC events
	peerConnection.OnICECandidate(func(ice *webrtc.ICECandidate) {
		if ice != nil {
			candidateJSON := ice.ToJSON()
			message := Message{
				Event: "ice-candidate",
				Data: map[string]any{
					"candidate": candidateJSON,
				},
			}
			client.sendJSON(message)
		}
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("Connection state changed: %s", state.String())
	})

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Received track: %s, %s", track.ID(), track.Kind())

		if track.Kind() != webrtc.RTPCodecTypeVideo {
			return
		}

		udpConn, err := net.Dial("udp", "127.0.0.1:5004") // GStreamer will listen on this port
		if err != nil {
			log.Fatal("Failed to open UDP socket:", err)
		}
		defer udpConn.Close()

		// Forward VP8 RTP packets to UDP
		for {
			rtpPacket, _, err := track.ReadRTP()
			if err != nil {
				log.Println("Error reading RTP packet:", err)
				return
			}

			// Convert RTP packet to bytes
			packetBytes, err := rtpPacket.Marshal()
			if err != nil {
				log.Println("Error marshaling RTP packet:", err)
				continue
			}

			// Send RTP packet to UDP socket (GStreamer)
			_, err = udpConn.Write(packetBytes)
			if err != nil {
				log.Println("Error sending RTP packet:", err)
			}
		}
	})

	// Handle WebSocket messages
	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("Error reading message: %v", err)
			break
		}

		log.Printf("Received event: %s", msg.Event)

		switch msg.Event {
		case "start-stream":
			// Stop any existing stream first
			if client.streamer != nil {
				client.streamer.Stop()
				// Give it a moment to clean up
				time.Sleep(time.Second)
			}

			// Create a new streamer
			client.streamer = ffmpeg.NewFFmpegStreamer()

			// Handle stream configuration
			streamType, ok := msg.Data["type"].(string)
			if !ok {
				log.Printf("Invalid stream type")
				client.sendJSON(Message{
					Event: "stream-error",
					Data: map[string]any{
						"error": "Invalid stream type",
					},
				})
				break
			}

			log.Printf("Starting stream type: %s", streamType)
			var err error

			switch streamType {
			case "webcam":
				// Handle webcam streaming
				inputDevice, ok := msg.Data["inputDevice"].(string)
				if !ok || inputDevice == "" {
					log.Printf("Invalid input device")
					client.sendJSON(Message{
						Event: "stream-error",
						Data: map[string]any{
							"error": "Invalid input device",
						},
					})
					break
				}

				log.Printf("Starting webcam stream from %s", inputDevice)
				err = client.streamer.StreamWebcam(inputDevice, VirtualCameraDevice)

			case "screen":
				// Handle screen capture streaming
				display, ok := msg.Data["inputDevice"].(string)
				if !ok || display == "" {
					log.Printf("Invalid display")
					client.sendJSON(Message{
						Event: "stream-error",
						Data: map[string]any{
							"error": "Invalid display",
						},
					})
					break
				}

				resolution, ok := msg.Data["resolution"].(string)
				if !ok || resolution == "" {
					log.Printf("Invalid resolution")
					client.sendJSON(Message{
						Event: "stream-error",
						Data: map[string]any{
							"error": "Invalid resolution",
						},
					})
					break
				}

				log.Printf("Starting screen capture from %s at %s", display, resolution)
				err = client.streamer.StreamScreen(display, resolution, VirtualCameraDevice)

			case "file":
				// Handle video file streaming
				inputFile, ok := msg.Data["inputDevice"].(string)
				if !ok || inputFile == "" {
					log.Printf("Invalid input file")
					client.sendJSON(Message{
						Event: "stream-error",
						Data: map[string]any{
							"error": "Invalid input file",
						},
					})
					break
				}

				log.Printf("Starting file stream from %s", inputFile)
				err = client.streamer.StreamVideoFile(inputFile, VirtualCameraDevice)

			default:
				log.Printf("Unknown stream type: %s", streamType)
				client.sendJSON(Message{
					Event: "stream-error",
					Data: map[string]any{
						"error": "Unknown stream type",
					},
				})
				break
			}

			if err != nil {
				log.Printf("Error starting stream: %v", err)
				client.sendJSON(Message{
					Event: "stream-error",
					Data: map[string]any{
						"error": err.Error(),
					},
				})
			} else {
				log.Printf("Stream started successfully")
				client.sendJSON(Message{
					Event: "stream-started",
					Data:  map[string]any{},
				})
			}

		case "stop-stream":
			// Stop the current stream if it exists
			log.Printf("Stopping stream")
			if client.streamer != nil {
				client.streamer.Stop()
				client.streamer = nil
				client.sendJSON(Message{
					Event: "stream-stopped",
					Data:  map[string]any{},
				})
			}
		case "offer":
			sdp, ok := msg.Data["sdp"].(string)
			if !ok {
				log.Println("Invalid SDP in offer")
				continue
			}

			err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  sdp,
			})
			if err != nil {
				log.Printf("Error setting remote description: %v", err)
				continue
			}

			// Create answer
			answer, err := peerConnection.CreateAnswer(nil)
			if err != nil {
				log.Printf("Error creating answer: %v", err)
				continue
			}

			err = peerConnection.SetLocalDescription(answer)
			if err != nil {
				log.Printf("Error setting local description: %v", err)
				continue
			}

			client.sendJSON(Message{
				Event: "answer",
				Data: map[string]any{
					"sdp": answer.SDP,
				},
			})

		case "ice-candidate":
			candidate, ok := msg.Data["candidate"].(map[string]any)
			if !ok {
				log.Println("Invalid ICE candidate")
				continue
			}

			candidateString, _ := json.Marshal(candidate)
			var iceCandidate webrtc.ICECandidateInit
			json.Unmarshal(candidateString, &iceCandidate)

			err := peerConnection.AddICECandidate(iceCandidate)
			if err != nil {
				log.Printf("Error adding ICE candidate: %v", err)
			}
		}
	}
}
