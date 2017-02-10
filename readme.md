# Sensible defaults -> no configuration files.

Yes.

# Boringstreamer streams mp3 files via http

Boringstreamer looks for mp3 files and broadcasts via http.
Browse to listen (e.g. http://localhost:4444/)

# Install

go get github.com/fgergo/boringstreamer

# Run

$ boringstreamer

or

c:\>boringstreamer

then use chrome (or firefox etc.)  to listen to music.

# Help

Use -h flag.

# Bug

A browser or player feature/bug: if the sample rate or bitrate or number of channels changes during streaming, the player stops playing the stream. Usually happens when boringstreamer streams different mp3s with different sample rates (e.g. 44100 and 48000). Boringstreamer handles the differences, but the browser or other hls client stops playing mp3s.

workaround 1: refresh page in the browser when mp3 playing is stopped.

workaround 2: change all mp3s to uniform format (doesn't matter which format, it just should be uniform) (e.g: ffmpeg -i source.mp3 -vn -ar 44100 -ac 2 -ab 128 -f mp3 output.mp3)
