// The MIT License (MIT)
//
// Copyright (c) 2015 Tristan Colgate-McFarlane and badgerodon
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

// Package mp3 provides decoding of mp3 files into thier underlying frames. It
// is primarily intended for streaming tasks with minimal internal buffering
// and no requirement to seek.
//
// The implementation started as a reworking of github.com/badgerodon/mp3, and
// has also drawn from Konrad Windszus' excellent article on mp3 frame parsing
// http://www.codeproject.com/Articles/8295/MPEG-Audio-Frame-Header
//
// TODO CRC isn't currently checked.
package mp3
