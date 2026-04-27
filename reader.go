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
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"strconv"
	"time"
)

// Reader reads and extracts .wpress archives.
type Reader struct {
	Filename      string
	File          *os.File
	NumberOfFiles int
	isV2          bool // true when the archive uses the v2 header format
}

// NewReader creates a new Reader and detects whether the archive is v1 or v2.
func NewReader(filename string) (*Reader, error) {
	r := &Reader{Filename: filename}
	if err := r.Init(); err != nil {
		return nil, err
	}
	return r, nil
}

// Init opens the archive file and detects the format version.
func (r *Reader) Init() error {
	file, err := os.Open(r.Filename)
	if err != nil {
		return err
	}
	r.File = file

	if err := r.detectVersion(); err != nil {
		// non-fatal: if detection fails we fall back to v1
		r.isV2 = false
	}
	return nil
}

// IsV2 reports whether the archive was identified as v2 format.
func (r *Reader) IsV2() bool {
	return r.isV2
}

// detectVersion seeks to the EOF block and checks for the v2 signature.
func (r *Reader) detectVersion() error {
	fi, err := r.File.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < headerSize {
		return nil
	}
	if _, err := r.File.Seek(-headerSize, io.SeekEnd); err != nil {
		return err
	}
	block := make([]byte, headerSize)
	if _, err := io.ReadFull(r.File, block); err != nil {
		return err
	}
	r.isV2 = IsV2EOFBlock(block)
	// restore position
	_, err = r.File.Seek(0, io.SeekStart)
	return err
}

// ExtractFile extracts the file matching filename and prefix path from the archive.
func (r Reader) ExtractFile(filename string, prefix string) ([]byte, error) {
	// TODO: implement
	return nil, nil
}

// Extract extracts all files to the current directory.
func (r Reader) Extract() (int, error) {
	return r.ExtractToPath(".")
}

// ExtractToPath extracts all files to outputPath.
// For v2 archives, the archive-level CRC32 is verified before extraction and
// per-file CRC32 values are checked after each file is written.
func (r Reader) ExtractToPath(outputPath string) (int, error) {
	if r.isV2 {
		if err := r.verifyArchiveCRC(); err != nil {
			return 0, err
		}
	}

	r.File.Seek(0, io.SeekStart)

	for {
		block, err := r.GetHeaderBlock()
		if err != nil {
			return 0, err
		}

		h := &Header{}

		if IsEOFBlock(block) {
			break
		}

		h.PopulateFromBytes(block, r.isV2)

		pathToFile := path.Clean(
			outputPath + string(os.PathSeparator) +
				string(bytes.Trim(h.Prefix, "\x00")) +
				string(os.PathSeparator) +
				string(bytes.Trim(h.Name, "\x00")),
		)

		if err := os.MkdirAll(path.Dir(pathToFile), 0755); err != nil {
			fmt.Println(err)
			return r.NumberOfFiles, err
		}

		file, err := os.Create(pathToFile)
		if err != nil {
			return r.NumberOfFiles, err
		}

		totalBytesToRead, _ := h.GetSize()
		for totalBytesToRead > 0 {
			bytesToRead := 512
			if bytesToRead > totalBytesToRead {
				bytesToRead = totalBytesToRead
			}
			content := make([]byte, bytesToRead)
			bytesRead, err := r.File.Read(content)
			if err != nil {
				return r.NumberOfFiles, err
			}
			totalBytesToRead -= bytesRead
			if _, err = file.Write(content[0:bytesRead]); err != nil {
				return r.NumberOfFiles, err
			}
		}
		file.Close()

		// v2: verify per-file CRC32 after extraction
		if r.isV2 && h.Crc32 != nil {
			expectedCRC := string(bytes.TrimRight(h.Crc32, "\x00"))
			if expectedCRC != "" {
				actualCRC, err := computeFileCRC32(pathToFile)
				if err == nil && actualCRC != expectedCRC {
					fmt.Printf("warning: CRC mismatch for %s: expected %s, got %s\n",
						pathToFile, expectedCRC, actualCRC)
				}
			}
		}

		r.NumberOfFiles++
	}

	return r.NumberOfFiles, nil
}

// GetHeaderBlock reads one 4377-byte header block from the current file position.
func (r Reader) GetHeaderBlock() ([]byte, error) {
	block := make([]byte, headerSize)
	bytesRead, err := r.File.Read(block)
	if err != nil {
		return nil, err
	}
	if bytesRead != headerSize {
		return nil, errors.New("unable to read header block size")
	}
	return block, nil
}

// GetFilesCount returns the number of file entries in the archive.
func (r Reader) GetFilesCount() (int, error) {
	if r.NumberOfFiles != 0 {
		return r.NumberOfFiles, nil
	}

	r.File.Seek(0, io.SeekStart)

	for {
		block, err := r.GetHeaderBlock()
		if err != nil {
			return 0, err
		}

		h := &Header{}
		if IsEOFBlock(block) {
			break
		}

		h.PopulateFromBytes(block, r.isV2)

		size, err := h.GetSize()
		if err != nil {
			return 0, err
		}
		r.File.Seek(int64(size), io.SeekCurrent)
		r.NumberOfFiles++
	}

	return r.NumberOfFiles, nil
}

// List returns a human-readable listing of all files in the archive.
// Each entry is formatted as: "<size> <mtime> <path>"
func (r *Reader) List() ([]string, error) {
	var fileList []string
	r.NumberOfFiles = 0

	if _, err := r.File.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	for {
		block, err := r.GetHeaderBlock()
		if err != nil {
			break
		}

		h := &Header{}
		if IsEOFBlock(block) {
			break
		}

		h.PopulateFromBytes(block, r.isV2)

		timestampStr := string(bytes.Trim(h.Mtime, "\x00"))
		formattedDate := timestampStr
		if ts, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
			formattedDate = time.Unix(ts, 0).Format("2006-01-02 15:04:05")
		}

		filePath := path.Clean(
			"." + string(os.PathSeparator) +
				string(bytes.Trim(h.Prefix, "\x00")) +
				string(os.PathSeparator) +
				string(bytes.Trim(h.Name, "\x00")),
		)

		entry := string(bytes.Trim(h.Size, "\x00")) + " " + formattedDate + " " + filePath
		if r.isV2 && h.Crc32 != nil {
			crc := string(bytes.TrimRight(h.Crc32, "\x00"))
			if crc != "" {
				entry += " crc32=" + crc
			}
		}
		fileList = append(fileList, entry)

		size, _ := h.GetSize()
		if _, err := r.File.Seek(int64(size), io.SeekCurrent); err != nil {
			return fileList, err
		}
		r.NumberOfFiles++
	}

	return fileList, nil
}

// verifyArchiveCRC computes the CRC32 of all bytes before the EOF block and
// compares it against the value stored in the EOF block. Returns an error on mismatch.
func (r Reader) verifyArchiveCRC() error {
	fi, err := r.File.Stat()
	if err != nil {
		return err
	}
	dataSize := fi.Size() - headerSize
	if dataSize <= 0 {
		return nil
	}

	// Read expected CRC from the EOF block
	if _, err := r.File.Seek(fi.Size()-headerSize, io.SeekStart); err != nil {
		return err
	}
	eofBlock := make([]byte, headerSize)
	if _, err := io.ReadFull(r.File, eofBlock); err != nil {
		return err
	}
	expectedCRC := string(eofBlock[4369:4377])

	// Hash all data before the EOF block
	if _, err := r.File.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := crc32.NewIEEE()
	if _, err := io.CopyN(h, r.File, dataSize); err != nil {
		return err
	}
	actualCRC := fmt.Sprintf("%08x", h.Sum32())

	if actualCRC != expectedCRC {
		return fmt.Errorf(
			"archive CRC32 mismatch: expected %s, got %s — the backup file may be damaged",
			expectedCRC, actualCRC,
		)
	}

	_, err = r.File.Seek(0, io.SeekStart)
	return err
}

// computeFileCRC32 returns the CRC32 checksum of a file as a lowercase 8-char hex string.
func computeFileCRC32(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08x", h.Sum32()), nil
}
