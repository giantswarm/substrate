// Copyright 2026 The Agent Substrate Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package snapshot

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// splitLegacyTar splits the tar every volume shared before the per-volume
// layout, whose entries are named <volume>/..., into one tar per volume at
// dsts[volume], named relative to the volume, with <volume>'s own entry as
// the "./" root entry. A volume with no entries gets no tar. It refuses an
// entry outside every volume of dsts and a hard link across volumes, which
// the split tars cannot represent. The 1.x worker split such a tar the same
// way on restore.
func splitLegacyTar(tarPath string, dsts map[string]string) (retErr error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	type out struct {
		f  *os.File
		bw *bufio.Writer
		tw *tar.Writer
	}
	outs := map[string]*out{}
	defer func() {
		for _, o := range outs {
			o.f.Close()
			if retErr != nil {
				os.Remove(o.f.Name())
			}
		}
	}()
	tr := tar.NewReader(bufio.NewReaderSize(f, 1<<20))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", legacyDurableTar, err)
		}
		name, skip, err := legacyEntryName(hdr.Name)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		top, rel, _ := strings.Cut(name, "/")
		dst, ok := dsts[top]
		if !ok {
			return fmt.Errorf("%s holds an entry outside every durable volume of the template", legacyDurableTar)
		}
		if rel == "" && hdr.Typeflag != tar.TypeDir {
			return fmt.Errorf("%s holds a top-level entry that is not a volume directory", legacyDurableTar)
		}
		split, err := splitHeader(hdr, top, rel)
		if err != nil {
			return err
		}
		o := outs[top]
		if o == nil {
			file, err := os.Create(dst)
			if err != nil {
				return err
			}
			bw := bufio.NewWriterSize(file, 1<<20)
			o = &out{f: file, bw: bw, tw: tar.NewWriter(bw)}
			outs[top] = o
		}
		if err := o.tw.WriteHeader(split); err != nil {
			return err
		}
		if _, err := io.Copy(o.tw, tr); err != nil {
			return err
		}
	}
	for _, o := range outs {
		if err := o.tw.Close(); err != nil {
			return err
		}
		if err := o.bw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// legacyEntryName returns the clean slash-separated name of an entry, and
// whether to skip it (the archive root).
func legacyEntryName(name string) (string, bool, error) {
	clean := strings.TrimPrefix(filepath.Clean(filepath.FromSlash(name)), string(filepath.Separator))
	if clean == "" || clean == "." {
		return "", true, nil
	}
	if !filepath.IsLocal(clean) {
		return "", false, fmt.Errorf("%s holds an entry that is not a local path", legacyDurableTar)
	}
	return filepath.ToSlash(clean), false, nil
}

// splitHeader returns hdr renamed to rel, its path below top. The PAX
// records keep only the xattrs: the reader also records the original path
// and link path there, which would override the new names.
func splitHeader(hdr *tar.Header, top, rel string) (*tar.Header, error) {
	out := *hdr
	out.Format = tar.FormatUnknown
	out.PAXRecords = nil
	for k, v := range hdr.PAXRecords {
		if strings.HasPrefix(k, "SCHILY.xattr.") {
			if out.PAXRecords == nil {
				out.PAXRecords = map[string]string{}
			}
			out.PAXRecords[k] = v
		}
	}
	switch {
	case rel == "":
		out.Name = "./"
	case hdr.Typeflag == tar.TypeDir:
		out.Name = rel + "/"
	default:
		out.Name = rel
	}
	if hdr.Typeflag == tar.TypeLink {
		target, skip, err := legacyEntryName(hdr.Linkname)
		if err != nil || skip {
			return nil, fmt.Errorf("%s holds an invalid hard link", legacyDurableTar)
		}
		targetTop, targetRel, _ := strings.Cut(target, "/")
		if targetTop != top || targetRel == "" {
			return nil, fmt.Errorf("%s holds a hard link across volumes", legacyDurableTar)
		}
		out.Linkname = targetRel
	}
	return &out, nil
}
