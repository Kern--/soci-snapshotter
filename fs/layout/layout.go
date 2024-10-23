package layout

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/awslabs/soci-snapshotter/util/image"
	"github.com/awslabs/soci-snapshotter/util/ioutils"
	"github.com/awslabs/soci-snapshotter/util/namedmutex"
	"github.com/awslabs/soci-snapshotter/ztoc"
	"github.com/awslabs/soci-snapshotter/ztoc/compression"
	"github.com/containerd/log"
	"golang.org/x/sys/unix"
)

type Status string

var (
	InProgress Status = "in progress"
	Complete   Status = "complete"
)

var layoutersMu namedmutex.NamedMutex
var layouters sync.Map

var ErrNotComplete = errors.New("file download is not complete")
var ErrNotRegular = errors.New("file is not a regular file")

type Layouter struct {
	root     string
	fmd      []ztoc.FileMetadata
	progress sync.Map
	fcache   sync.Map
	n        int
	writer   chan wf
}

type wf struct {
	offset compression.Offset
	data   []byte
}

func (l *Layouter) writeLoop() {
	for wf := range l.writer {
		go func() {
			err := l.write(wf.offset, wf.data)
			if err != nil {
				log.L.Errorf("error writing data: %v", err)
			}
		}()
	}
}

func New(root string, toc ztoc.TOC) (*Layouter, error) {
	layoutersMu.Lock(root)
	defer layoutersMu.Unlock(root)

	if l, ok := layouters.Load(root); ok {
		log.L.Debugf("using cached layouter for %s", root)
		return l.(*Layouter), nil
	}
	log.L.Debugf("creating layouter for %s", root)

	l := Layouter{
		root:     root,
		fmd:      toc.FileMetadata,
		progress: sync.Map{},
		fcache:   sync.Map{},
		n:        0,
		writer:   make(chan wf, 100),
	}
	for _, ent := range toc.FileMetadata {
		l.progress.Store(ent.Name, ent.UncompressedSize)
	}

	layouters.Store(root, &l)

	go func() {
		l.layoutRoot()
		l.writeLoop()
	}()
	return &l, nil
}

func (l *Layouter) edge(offset compression.Offset) (ztoc.FileMetadata, ztoc.FileMetadata, error) {
	for i, edge := range l.fmd {
		if edge.TarHeaderOffset <= offset {
			if i == len(l.fmd)-1 || offset < l.fmd[i+1].TarHeaderOffset {
				var next ztoc.FileMetadata
				if i != len(l.fmd)-1 {
					next = l.fmd[i+1]
				}
				return edge, next, nil
			}
		}
	}
	return ztoc.FileMetadata{}, ztoc.FileMetadata{}, os.ErrNotExist
}

func (l *Layouter) writeFile(path string, offset int64, r io.Reader) error {
	f, err := l.open(path, false)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	// todo, handle short writes
	n, err := io.Copy(f, r)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	err = unix.Fsync(int(f.Fd()))
	for {
		v, _ := l.progress.Load(path)
		nv := v.(compression.Offset) - compression.Offset(n)
		if l.progress.CompareAndSwap(path, v, nv) {
			break
		}
	}

	return err
}

func (l *Layouter) write(offset compression.Offset, data []byte) error {
	e, next, err := l.edge(offset)
	if err != nil {
		return err
	}
	r := bytes.NewReader(data)

	cr := ioutils.NewPositionTrackerReader(r)

	if e.TarHeaderOffset != offset {
		// consume remaning tar header
		var hdrRemaining compression.Offset
		if e.UncompressedOffset > offset {
			hdrRemaining := e.UncompressedOffset - offset
			io.Copy(io.Discard, io.LimitReader(cr, int64(hdrRemaining)))
		}

		// consume remaning
		remaining := (e.UncompressedOffset + e.UncompressedSize) - (offset + hdrRemaining)
		if remaining > 0 {
			lr := io.LimitReader(cr, int64(remaining))
			if e.Type == "reg" && !image.IsWhiteout(e.Name) {
				l.writeFile(e.Name, int64(e.UncompressedSize)-int64(remaining), lr)
			} else {
				io.Copy(io.Discard, lr)
			}
		}

		if next.TarHeaderOffset != 0 {
			padding := next.TarHeaderOffset - (offset + compression.Offset(cr.CurrentPos()))
			io.Copy(io.Discard, io.LimitReader(cr, int64(padding)))
		}
	}

	tarReader := tar.NewReader(r)
	for {
		hdr, err := tarReader.Next()
		if err != nil {
			// Failure modes:
			// 1. EOF because we completely consumed the datastream
			// 2. UnexpectedEOF because there aren't enough bytes for a full tar header
			// 3. tar.ErrHeader because we read a partial header, but it's invalid
			// 4. tar.ErrHeader because we read a full header, but it's invalid
			//
			// This ignores error case 4.
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, tar.ErrHeader) {
				break
			}
			return err
		}
		if hdr.Typeflag != tar.TypeReg || image.IsWhiteout(hdr.Name) {
			io.Copy(io.Discard, io.LimitReader(tarReader, hdr.Size))
			continue
		}
		lr := io.LimitReader(tarReader, hdr.Size)
		err = l.writeFile(hdr.Name, 0, lr)
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Layouter) Write(offset compression.Offset, data []byte) error {
	l.writer <- wf{offset, data}
	return nil
}

func (l *Layouter) open(path string, requireComplete bool) (*os.File, error) {
	if requireComplete {
		n, ok := l.progress.Load(path)
		if !ok || n.(compression.Offset) > 0 {
			return nil, ErrNotComplete
		}

		f, ok := l.fcache.Load(path)
		if ok {
			return f.(*os.File), nil
		}
	}

	oldPath := path

	path, err := l.realPath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	if requireComplete {
		f2, ok := l.fcache.LoadOrStore(oldPath, f)
		if ok {
			f.Close()
		}
		return f2.(*os.File), nil
	}
	return f, nil
}

func (l *Layouter) Open(path string) (*os.File, error) {
	return l.open(path, true)
}

func (l *Layouter) Root() string {
	return l.root
}

func (l *Layouter) Status() Status {
	return Complete
}
