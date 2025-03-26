* RTMP solution

- RUN RTMP server via ngix

- RUN UDP server
    ```
    gst-launch-1.0 udpsrc port=5004 caps="application/x-rtp, encoding-name=VP8, payload=96"     ! rtpvp8depay ! vp8dec ! videoscale ! videoconvert     ! video/x-raw,width=494,height=370     ! x264enc tune=zerolatency     ! flvmux ! rtmpsink location="rtmp://localhost:1935/live/stream"
    ```

- start webrtc server
    ```
    go run cmd/rtmpsolution/main.go
    ```
- open browser, localhost:8080
- click on start
- click on call after ws connected
- start OBS virtual camera. and get the device, like /dev/video2

