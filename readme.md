Sensible defaults -> no configuration files.

# Boringstreamer streams mp3 files via http

Boringstreamer looks for mp3 files and broadcasts via http.
Browse to listen (e.g. http://localhost:4444/)

# Install

set GO111MODULE=on (windows)

or

export GO111MODULE=on (others)

go get github.com/fgergo/boringstreamer

# Run

$ boringstreamer

or

c:\\>boringstreamer

then use chrome (or firefox etc.)  to listen to music.

# Help

Use -h flag.

# Bug

A browser or player feature/bug:  Usually happens when boringstreamer streams
different mp3s with different sample rates (e.g. 44100 and 48000). If the sample
rate or bitrate or number of channels changes during playing, the player stops
playing the stream. Boringstreamer handles the different files, but the players stop playing mp3s.

Workaround 1: Refresh page in the browser when mp3 playing is stopped.

Workaround 2: Change all mp3s to uniform format. Doesn't matter which format, it just should be uniform.
For example with ffmpeg:

	ffmpeg -i source.mp3 -vn -ar 44100 -ac 2 -ab 128 -f mp3 output.mp3
