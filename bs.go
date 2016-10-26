// Author: Gergely Födémesi fgergo@gmail.com

// Boringstreamer looks for mp3 files and broadcasts via http (live streaming.)
//
// $ boringstreamer
//
// or
//
// c:\>boringstreamer.exe
//
// recursively looks for .mp3 files starting from "/" and broadcasts on port 4444 for at most 42 concurrent http clients.
//
// See -h for details.
//
// Browse to listen (e.g. http://localhost:4444/)
package main

import (
	"bytes"
	"bufio"
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

	_ "net/http/pprof"		// TODO(fgergo) remove when finished
)

var (
	addr           = flag.String("addr", ":4444", "listen on address (:port or host:port)")
	maxConnections = flag.Int("max", 42, "set maximum number of streaming connections")
	recursively    = flag.Bool("r", true, "recursively look for music starting from path")
	verbose        = flag.Bool("v", false, "display verbose messages")
)

var debugging bool // controlled by hidden command line argument -debug

// like /dev/null
type nullWriter struct {}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type streamFrame []byte

// client's event
type broadcastResult struct {
	qid int
	err error
}

// After a start() mux broadcasts audio stream to subscribed clients (ie. to http servers).
// Clients subscribe() and unsubscribe by writing to result chanel.
type mux struct {
	sync.Mutex

	clients map[int]chan streamFrame // set of listener clients to be notified
	result  chan broadcastResult     // clients share broadcast success-failure here
}

// subscribe(ch) adds ch to the set of channels to be received on by the clients when a new audio frame is available.
// Returns uniq client id (qid) for ch and a broadcast result channel for the client.
// Returns -1, nil if too many clients are already listening.
// clients: qid, br := m.subscribe(ch)
func (m *mux) subscribe(ch chan streamFrame) (int, chan broadcastResult) {
	m.Lock()
	// search for available qid
	qid := 0
	_, ok := m.clients[qid]
	for ; ok; _, ok = m.clients[qid] {
		if qid >= *maxConnections-1 {
			m.Unlock()
			return -1, nil
		}
		qid++
	}
	m.clients[qid] = ch
	m.Unlock()
	if *verbose {
		fmt.Printf("New connection (qid: %v), streaming to %v connections, at %v\n", qid, len(m.clients), time.Now().Format(time.Stamp))
	}

	return qid, m.result
}

// start() initializes a multiplexer for raw audio streams
// e.g: m := new(mux).start(path)
func (m *mux) start(path string) *mux {
	m.result = make(chan broadcastResult)
	m.clients = make(map[int]chan streamFrame)

	// flow structure: fs -> nextFile -> nextStream -> nextFrame -> subscribed http servers -> browsers
	nextFile := make(chan string)       // next file to be broadcast
	nextStream := make(chan io.Reader)  // next raw audio stream
	nextFrame := make(chan streamFrame) // next audio frame

	// generate randomized list of files available from path
	rand.Seed(time.Now().Unix()) // minimal randomness
	rescan := make(chan chan string)
	go func() {
		if path == "-" {
			return
		}

		for {
			files := <-rescan

			t0 := time.Now()
			notified := false
			filepath.Walk(path, func(wpath string, info os.FileInfo, err error) error {
				// notify user if no audio files are found after 4 seconds of walking path recursively
				dt := time.Now().Sub(t0)
				if dt > 4*time.Second && !notified && *verbose {
					fmt.Printf("Still looking for first audio file under %#v to broadcast, after %v... Maybe try -h flag.\n", path, dt)
					notified = true
				}

				if err != nil {
					return nil
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				probably := strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") // probably mp3
				if !info.IsDir() && !probably {
					return nil
				}

				files <- wpath // found file

				return nil
			})
			close(files)
			time.Sleep(1 * time.Second) // if no files are found, poll at least with 1Hz
		}
	}()

	// buffer and shuffle
	go func() {
		if path == "-" {
			return
		}

		for {
			files := make(chan string)
			rescan <- files

			shuffled := make([]string, 0) // randomized set of files

			for f := range files {
				select {
				case <-time.After(100 * time.Millisecond): // start playing as soon as possible, but wait at least 0.1 second for shuffling
					nextFile <- f
					if *verbose {
						fmt.Printf("Next: %v\n", f)
					}
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
				nextFile <- f
				if *verbose {
					fmt.Printf("Next: %v\n", f)
				}
			}
		}
	}()

	// open file
	go func() {
		if path == "-" {
			nextStream <- os.Stdin
			return
		}

		for {
			filename := <-nextFile
			f, err := os.Open(filename)
			if err != nil {
				if debugging {
					log.Printf("Skipped \"%v\", err=%v", filename, err)
				}
				continue
			}
			nextStream <- bufio.NewReaderSize(f, 1024*1024)
			if *verbose {
				fmt.Printf("Now playing: %v\n", filename)
			}
		}
	}()

	// decode stream to frames and delay for frame duration
	go func() {
		nullwriter := new(nullWriter)
		var cumwait time.Duration
		for {
			streamReader := <-nextStream
			d := mp3.NewDecoder(streamReader)
			var f mp3.Frame
			for {
				t0 := time.Now()
				tmp := log.Prefix()
				if !debugging {
					log.SetOutput(nullwriter) // hack to silence mp3 debug/log output
				} else {
					log.SetPrefix("info: mp3 decode msg: ")
				}
				err := d.Decode(&f)
				log.SetPrefix(tmp)
				if !debugging {
					log.SetOutput(os.Stderr)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					if debugging {
						log.Printf("Skipping frame, d.Decode() err=%v", err)
					}
					continue
				}
				buf, err := ioutil.ReadAll(f.Reader())
				if err != nil {
					if debugging {
						log.Printf("Skipping frame, ioutil.ReadAll() err=%v", err)
					}
					continue
				}
				nextFrame <- buf

				towait := f.Duration() - time.Now().Sub(t0)
				cumwait += towait // towait can be negative -> cumwait
				if cumwait > 1*time.Second {
					time.Sleep(cumwait)
					cumwait = 0
				}
			}
		}
	}()

	// broadcast frame to clients
	go func() {
		for {
			f := <-nextFrame
			// notify clients of new audio frame or let them quit
			m.Lock()
			for _, ch := range m.clients {
				m.Unlock()
				ch <- f
				br := <-m.result // handle quitting clients
				if br.err != nil {
					m.Lock()
					close(m.clients[br.qid])
					delete(m.clients, br.qid)
					nclients := len(m.clients)
					m.Unlock()
					if debugging {
						log.Printf("Connection exited, qid: %v, error %v. Now streaming to %v connections.", br.qid, br.err, nclients)
					} else if *verbose {
						fmt.Printf("Connection exited, qid: %v. Now streaming to %v connections, at %v\n", br.qid, nclients, time.Now().Format(time.Stamp))
					}
				}
				m.Lock()
			}
			m.Unlock()
		}
	}()

	return m
}

type streamHandler struct {
	*mux
}

// chrome and firefox play mp3 audio stream directly
// details: https://tools.ietf.org/html/draft-pantos-http-live-streaming-20
// search for "Packed Audio"
func (sh streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	frames := make(chan streamFrame)
	qid, br := sh.subscribe(frames)
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

	// some browsers need ID3 tag to identify first frame as audio media to be played
	// minimal ID3 header to designate audio stream
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
		fmt.Printf("Usage: %s [flags] [path]\n", os.Args[0])
		fmt.Println("then browse to listen. (e.g. http://localhost:4444/)")
		fmt.Printf("%v does not follow links.\n", os.Args[0])
		fmt.Printf("To stream from standard input: %v -\n\n", os.Args[0])
		fmt.Println("flags:")
		flag.PrintDefaults()
	}
	flag.Parse()
	if len(flag.Args()) > 1 && flag.Args()[1] != "-debug" {
		flag.Usage()
		os.Exit(1)
	}

	path := "/"
	switch len(flag.Args()) {
	case 0:
		if *verbose {
			fmt.Printf("Using path %#v, see -h for details.\n", path)
		}
	case 1:
		path = flag.Args()[0]
	case 2:
		path = flag.Args()[0]
		debugging = true
	}

	// check if path is available
	if path != "-" {
		matches, err := filepath.Glob(path)
		if err != nil || len(matches) < 1 {
			fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable, nothing to play.\n", path)
			os.Exit(1)
		}

		err = os.Chdir(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable, nothing to play. Error: %v\n", path, err)
			os.Exit(1)
		}
		path, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable, nothing to play. Error: %v\n", path, err)
			os.Exit(1)
		}

		if *verbose {
			fmt.Printf("Looking for files available from \"%v\" ...\n", path)
		}
	}

	if *verbose {
		fmt.Printf("Waiting for connections on %v\n", *addr)
	}

	if debugging {
		// TODO(fgergo), remove when finished
		// start profile serving page: http://ip.ad.dr.ess:6060/debug/pprof
		go func() {
			log.Println(http.ListenAndServe(":6060", nil))
		}()
	}
	
	// initialize and start mp3 streamer
	err := http.ListenAndServe(*addr, streamHandler{new(mux).start(path)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Exiting, error: %v\n", err) // log.Fatalf() race with log.SetPrefix()
		os.Exit(1)
	}
}
