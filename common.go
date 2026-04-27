/**
 * The MIT License (MIT)
 *
 * Copyright (c) 2014 Yani Iliev <yani@iliev.me>
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package wpress

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const (
	headerSize   = 4377 // total length of a header block
	filenameSize = 255  // max bytes for filename
	contentSize  = 14   // max bytes for file size field
	mtimeSize    = 12   // max bytes for last modified date
	prefixSize   = 4096 // max bytes for path prefix (v1)

	// v2 field layout within the header block:
	//   [0:255]    filename        (255 bytes, null-terminated)
	//   [255:269]  size            (14 bytes,  null-terminated)
	//   [269:281]  mtime           (12 bytes,  null-terminated)
	//   [281:4369] path prefix     (4088 bytes, null-terminated)
	//   [4369:4377] crc32          (8 bytes,   hex ASCII, no null terminator)
	prefixSizeV2 = 4088 // max bytes for path prefix in v2
	crc32Size    = 8    // bytes reserved for CRC32 hex string in v2
)

var hexPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

// Header represents the metadata block that precedes each file in a .wpress archive.
//
// v1 layout (4377 bytes):
//
//	Name   [0:255]    filename, null-padded
//	Size   [255:269]  decimal file size, null-padded
//	Mtime  [269:281]  unix timestamp string, null-padded
//	Prefix [281:4377] directory path, null-padded (4096 bytes)
//
// v2 layout (4377 bytes):
//
//	Name   [0:255]    filename, null-padded
//	Size   [255:269]  decimal file size, null-padded
//	Mtime  [269:281]  unix timestamp string, null-padded
//	Prefix [281:4369] directory path, null-padded (4088 bytes)
//	Crc32  [4369:4377] CRC32 hex string (8 ASCII bytes)
type Header struct {
	Name   []byte
	Size   []byte
	Mtime  []byte
	Prefix []byte
	Crc32  []byte // nil for v1; 8-byte hex ASCII for v2
}

// PopulateFromBytes populates the header from a raw 4377-byte block.
// Pass isV2=true to parse the v2 layout (shorter Prefix + Crc32 field).
func (h *Header) PopulateFromBytes(block []byte, isV2 bool) {
	h.Name = block[0:255]
	h.Size = block[255:269]
	h.Mtime = block[269:281]
	if isV2 {
		h.Prefix = block[281:4369]
		h.Crc32 = block[4369:4377]
	} else {
		h.Prefix = block[281:4377]
		h.Crc32 = nil
	}
}

// PopulateFromFilename populates the header from a file on disk (v1 format).
func (h *Header) PopulateFromFilename(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	fi, err := file.Stat()
	if err != nil {
		return err
	}
	file.Close()

	if len(fi.Name()) > filenameSize {
		return errors.New("filename is longer than max allowed")
	}
	h.Name = make([]byte, filenameSize)
	copy(h.Name, fi.Name())

	size := strconv.FormatInt(fi.Size(), 10)
	if len(size) > contentSize {
		return errors.New("file size is larger than max allowed")
	}
	h.Size = make([]byte, contentSize)
	copy(h.Size, size)

	unixTime := strconv.FormatInt(fi.ModTime().Unix(), 10)
	if len(unixTime) > mtimeSize {
		return errors.New("last modified date is after max allowed")
	}
	h.Mtime = make([]byte, mtimeSize)
	copy(h.Mtime, unixTime)

	_path := filepath.Dir(filename)
	if len(_path) > prefixSize {
		return errors.New("prefix size is longer than max allowed")
	}
	h.Prefix = make([]byte, prefixSize)
	copy(h.Prefix, _path)

	return nil
}

// GetHeaderBlock serialises the header to bytes (always v1 format).
func (h Header) GetHeaderBlock() []byte {
	block := append(h.Name, h.Size...)
	block = append(block, h.Mtime...)
	block = append(block, h.Prefix...)
	return block
}

// GetSize returns the file content size stored in the header.
func (h Header) GetSize() (int, error) {
	return strconv.Atoi(string(bytes.Trim(h.Size, "\x00")))
}

// GetEOFBlock returns a v1 EOF marker: 4377 zero bytes.
func (h Header) GetEOFBlock() []byte {
	return bytes.Repeat([]byte("\x00"), headerSize)
}

// IsEOFBlock reports whether block is a valid EOF marker (v1 or v2).
func IsEOFBlock(block []byte) bool {
	if len(block) != headerSize {
		return false
	}
	if IsV2EOFBlock(block) {
		return true
	}
	return bytes.Equal(block, bytes.Repeat([]byte("\x00"), headerSize))
}

// IsV2EOFBlock reports whether block is a v2 EOF marker.
//
// v2 EOF structure:
//
//	[0:255]    all zero        (empty filename signals EOF)
//	[255:269]  non-empty size  (archive-level CRC payload size)
//	[269:4369] arbitrary
//	[4369:4377] 8 hex digits   (CRC32 of all data before the EOF block)
func IsV2EOFBlock(block []byte) bool {
	if len(block) != headerSize {
		return false
	}
	if !bytes.Equal(block[0:255], bytes.Repeat([]byte("\x00"), 255)) {
		return false
	}
	sizeField := string(bytes.Trim(block[255:269], "\x00"))
	if sizeField == "" {
		return false
	}
	return hexPattern.MatchString(string(block[4369:4377]))
}
