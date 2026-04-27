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
	"os"
)

// Writer writes .wpress archives in v1 format.
type Writer struct {
	Filename   string
	File       *os.File
	FilesAdded int
}

// NewWriter creates a new Writer instance.
func NewWriter(filename string) (*Writer, error) {
	w := &Writer{Filename: filename}
	if err := w.Init(); err != nil {
		return nil, err
	}
	return w, nil
}

// Init creates the archive file on disk.
func (w *Writer) Init() error {
	file, err := os.Create(w.Filename)
	if err != nil {
		return err
	}
	w.File = file
	return nil
}

// AddFile appends a single file to the archive.
func (w *Writer) AddFile(filename string) error {
	h := &Header{}
	if err := h.PopulateFromFilename(filename); err != nil {
		return err
	}

	if _, err := w.File.Write(h.GetHeaderBlock()); err != nil {
		return err
	}

	input, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer input.Close()

	buf := make([]byte, 512)
	for {
		n, err := input.Read(buf)
		if n > 0 {
			if _, werr := w.File.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
	}

	w.FilesAdded++
	return nil
}

// AddDirectory recursively adds all files in a directory to the archive.
func (w *Writer) AddDirectory(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := dirPath + string(os.PathSeparator) + entry.Name()
		if entry.IsDir() {
			if err := w.AddDirectory(fullPath); err != nil {
				return err
			}
		} else {
			if err := w.AddFile(fullPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close appends the EOF marker and closes the archive.
// No EOF marker is written if no files were added.
func (w Writer) Close() error {
	if w.FilesAdded == 0 {
		return nil
	}
	h := &Header{}
	if _, err := w.File.Write(h.GetEOFBlock()); err != nil {
		return err
	}
	return w.File.Close()
}
