// Boringstreamer looks for mp3 files and broadcasts via http.
// $ boringstreamer -addr 4444 -max 42 /
// recursively looks for mp3 files starting from / and broadcasts on port 4444 for at most 42 concurrent streamer clients.
// Browse to listen (e.g. http://localhost:4444/)
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tcolgate/mp3"
)

var (
	addr           = flag.String("addr", ":4444", "listen on address (format: :port or host:port)")
	maxConnections = flag.Int("max", 42, "set maximum number of streaming connections")
	recursively    = flag.Bool("r", true, "recursively look for music starting from path")
	verbose        = flag.Bool("v", false, "display verbose messages")
)

// like /dev/null
type nullWriter struct {
}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type streamFrame []byte

// client's event
type broadcastResult struct {
	qid int
	err error
}

// After a start() mux broadcasts audio stream to it's listener clients.
// Clients subscribe() and unsubscribe by writing to result chanel.
type mux struct {
	sync.Mutex

	clients map[int]chan streamFrame // set of listener clients to be notified
	result  chan broadcastResult     // clients share broadcast success-failure here

	nextFile   chan string      // next file to be broadcast
	nextStream chan io.Reader   // next (ID3 stripped) raw audio stream
	nextFrame  chan streamFrame // next audio frame
}

// subscribe(ch) adds ch to the set of channels to be received on by the clients when a new audio frame is available.
// Returns uniq client id (qid) for ch and a broadcast result channel for the client.
// Returns -1, nil if too many clients are already listening.
// clients: qid, br := m.subscribe(ch)
func (m *mux) subscribe(ch chan streamFrame) (int, chan broadcastResult) {
	m.Lock()
	defer m.Unlock()
	// search for available qid
	qid := 0
	_, ok := m.clients[qid]
	for ; ok; _, ok = m.clients[qid] {
		if qid >= *maxConnections-1 {
			return -1, nil
		}
		qid++
	}
	m.clients[qid] = ch
	if *verbose {
		log.Printf("New connection (qid: %v), streaming to %v connections.", qid, len(m.clients))
	}

	return qid, m.result
}

// stripID3Header(r) reads file from r, strips id3v2 headers and returns the rest
// id3v2 tag details: id3.org
func stripID3Header(r io.Reader) io.Reader {
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		log.Printf("Error: skipping file, stripID3Header(), err=%v", err)
		return bytes.NewReader(make([]byte, 0))
	}

	// TODO(fgergo) add ID3 v1 detection
	if string(buf[:3]) != "ID3" {
		return bytes.NewReader(buf) // no ID3 header
	}

	// The ID3v2 tag size is encoded in four bytes
	// where msb (bit 7) is set to zero in every byte,
	// ie. tag size is at most 2^28 (4*8-4=28).
	id3size := int32(buf[6])<<21 | int32(buf[7])<<14 | int32(buf[8])<<7 | int32(buf[9])
	id3size += 10 // calculated tag size is excluding the header => +10

	return bytes.NewReader(buf[id3size:])
}

// genFileList() periodically checks for files available from root and
// sends filenames down chan queue.
func genFileList(root string, queue chan string) {
	rand.Seed(time.Now().Unix()) // minimal randomness

	rescan := make(chan chan string)
	go func() {
		for {
			files := <-rescan
			filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				ok := strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") // probably file is mp3
				if !info.IsDir() && !ok {
					return nil
				}
				files <- path // found file

				return nil
			})
			close(files)
			time.Sleep(1 * time.Second) // poll at least with 1Hz
		}
	}()

	// buffer and shuffle
	go func() {
		for {
			files := make(chan string)
			rescan <- files

			shuffled := make([]string, 0) // randomized set of files

			for f := range files {
				select {
				case queue <- f: // start playing as soon as possible
					if *verbose {
						fmt.Printf("Next: %v\n", f)
					}
					continue
				default:
					// shuffle files for random playback
					// (random permutation)
					if len(shuffled) == 0 {
						shuffled = append(shuffled, f)
					} else {
						i := rand.Intn(len(shuffled))
						shuffled = append(shuffled, shuffled[i])
						shuffled[i] = f
					}
				}
			}

			// queue shuffled files
			for _, f := range shuffled {
				queue <- f
				if *verbose {
					fmt.Printf("Next: %v\n", f)
				}
			}
		}
	}()
}

// start() initializes a multiplexer for raw audio streams
// e.g: m := new(mux).start(path)
func (m *mux) start(path string) *mux {
	m.result = make(chan broadcastResult)
	m.clients = make(map[int]chan streamFrame)

	m.nextFile = make(chan string)
	m.nextStream = make(chan io.Reader)
	m.nextFrame = make(chan streamFrame)

	// generate randomized list of files available from path
	genFileList(path, m.nextFile)

	// read file, strip ID3 header
	go func() {
		for {
			filename := <-m.nextFile
			f, err := os.Open(filename)
			if err != nil {
				log.Printf("Skipped \"%v\", err=%v", filename, err)
				continue
			}
			m.nextStream <- stripID3Header(f)
			if *verbose {
				fmt.Printf("Now playing: %v\n", filename)
			}
		}
	}()

	// decode stream to frames
	go func() {
		nullwriter := new(nullWriter)
		for {
			streamReader := <-m.nextStream
			d := mp3.NewDecoder(streamReader)
			var f mp3.Frame
			//			sent := 0 // TODO(fgergo) remove later
			//			lastSent := time.Now().UTC()
			for {
				t0 := time.Now()
				tmp := log.Prefix()
				if !*verbose {
					log.SetOutput(nullwriter) // hack to silence mp3 debug/log output
				} else {
					log.SetPrefix("info: mp3 decode msg: ")
				}
				err := d.Decode(&f)
				log.SetPrefix(tmp)
				if !*verbose {
					log.SetOutput(os.Stderr)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					if *verbose {
						log.Printf("Skipping frame, d.Decode() err=%v", err)
					}
					continue
				}
				buf, err := ioutil.ReadAll(f.Reader())
				if err != nil {
					if *verbose {
						log.Printf("Skipping frame, ioutil.ReadAll() err=%v", err)
					}
					continue
				}
				m.nextFrame <- buf

				/*
					sent += len(buf)
					if sent >= 1*1024*1024 {
						now := time.Now().UTC()
						dur := now.Sub(lastSent)
						kBps := int64(sent)*1e9/1024/dur.Nanoseconds()
						if *verbose {
							log.Printf("Info: sent %#v bytes in the last %v (%vkB/sec)", sent, dur, int(kBps))
						}
						lastSent = now
						sent = 0
					}
				*/
				towait := f.Duration() - time.Now().Sub(t0)
				if towait > 0 {
					time.Sleep(towait)
				}
			}
		}
	}()

	// broadcast frame to clients
	go func() {
		for {
			f := <-m.nextFrame
			// notify clients of new audio frame or let them quit
			for _, ch := range m.clients {
				ch <- f
				br := <-m.result // handle quitting clients
				if br.err != nil {
					m.Lock()
					close(m.clients[br.qid])
					delete(m.clients, br.qid)
					m.Unlock()
					if *verbose {
						log.Printf("Connection exited, qid: %v, error %v. Now streaming to %v connections.", br.qid, br.err, len(m.clients))
					}
				}
			}
		}
	}()

	return m
}

type streamHandler struct {
	stream *mux
}

// chrome and firefox play mp3 audio stream directly
func (sh streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	frames := make(chan streamFrame)
	qid, br := sh.stream.subscribe(frames)
	if qid < 0 {
		log.Printf("Error: new connection request denied, already serving %v connections. See -h for details.", *maxConnections)
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Date", now.Format(http.TimeFormat))
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Server", "BoringStreamer/4.0")

	// browsers need ID3 tag to identify frames as media to be played
	// minimal id3 header to designate mp3 stream
	b := []byte{0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := io.Copy(w, bytes.NewReader(b))
	if err == nil {
		// broadcast mp3 stream to w
		broadcastTimeout := 4 * time.Second // timeout for slow clients
		result := make(chan error)
		for {
			buf := <-frames
			go func(r chan error, b []byte) {
				_, err = io.Copy(w, bytes.NewReader(b))
				if err == nil {
					w.(http.Flusher).Flush()
				}
				r <- err
			}(result, buf)
			select {
			case err = <-result:
				if err != nil {
					break
				}
				br <- broadcastResult{qid, nil} // frame streamed, no error, send ack
			case <-time.After(broadcastTimeout): // it's an error if io.Copy() is not finished within broadcastTimeout, ServeHTTP should exit
				err = errors.New(fmt.Sprintf("timeout: %v", broadcastTimeout))
			}
			if err != nil {
				break
			}
		}
	}
	br <- broadcastResult{qid, err} // error, send nack
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [path]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Browse to listen (e.g. http://localhost:4444/)\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if len(flag.Args()) > 1 {
		flag.Usage()
		os.Exit(1)
	}

	path := ""
	switch len(flag.Args()) {
	case 0:
		path = "."
		if *verbose {
			fmt.Printf("Using path %#v, see -h for details.\n", path)
		}
	case 1:
		path = flag.Args()[0]
	}

	if *verbose {
		fmt.Printf("Looking for files available from \"%v\" ...\n", path)
	}

	// check if path is available
	matches, err := filepath.Glob(path)
	if err != nil || len(matches) != 1 {
		fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable.\n", path)
		os.Exit(1)
	}

	// initialize and start mp3 streamer
	http.Handle("/", streamHandler{new(mux).start(path)})
	if *verbose {
		fmt.Printf("Waiting for connections on %v\n", *addr)
	}

	err = http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal(err)
	}
}
