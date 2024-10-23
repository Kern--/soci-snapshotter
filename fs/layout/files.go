package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/awslabs/soci-snapshotter/util/image"
	"github.com/awslabs/soci-snapshotter/ztoc"
	"github.com/containerd/continuity/fs"
	"github.com/containerd/log"
	"golang.org/x/sys/unix"
)

var errNotReady = errors.New("not ready to layout")
var errWhiteout = errors.New("file is a whiteout")

func (l *Layouter) layoutRoot() error {
	err := os.MkdirAll(l.root, 0755)
	if err != nil {
		return err
	}
	dag := l.fmd
	progress := true
	for len(dag) > 0 && progress {
		cur := dag
		dag = nil
		progress = false
		for _, md := range cur {
			err := l.layoutFile(md)
			if err != nil {
				if !errors.Is(err, errNotReady) {
					return err
				}
				dag = append(dag, md)
				continue
			}
			progress = true
		}
	}
	if !progress {
		return errors.New("unsatisfiable toc")
	}
	return nil
}

func (l *Layouter) realPath(path string) (string, error) {
	dir, file := filepath.Split(path)
	dir, err := fs.RootPath(l.root, dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, file), nil
}

func (l *Layouter) layoutFile(md ztoc.FileMetadata) error {
	path, err := l.realPath(md.Name)
	if err != nil {
		return err
	}
	log.L.Debugf("layout: %s", path)

	switch md.Type {
	case "hardlink":
		err = l.layoutHardLink(path, md)
	case "symlink":
		err = l.layoutSymLink(path, md)
	case "dir":
		err = l.layoutDir(path, md)
	case "reg":
		err = l.layoutRegFile(path, md)
	case "char":
		err = l.layoutCharDevice(path, md)
	case "block":
		err = l.layoutBlockDevice(path, md)
	case "fifo":
		err = l.layoutFifo(path, md)
	default:
		return fmt.Errorf("unknown type: %s", md.Type)
	}

	if err != nil {
		if errors.Is(err, errWhiteout) {
			return nil
		}
		return fmt.Errorf("failed to layout file: %w (%v)", err, md)
	}
	err = os.Lchown(path, md.UID, md.GID)
	if err != nil {
		return err
	}
	if md.Type != "symlink" {
		err = os.Chmod(path, md.FileMode())
		if err != nil {
			return err
		}
	}
	return unix.Lutimes(path, []unix.Timeval{
		unix.Timeval{},
		unix.NsecToTimeval(md.ModTime.UnixNano()),
	})
}

func (l *Layouter) layoutHardLink(path string, md ztoc.FileMetadata) error {
	oldname, err := l.realPath(md.Linkname)
	if err != nil {
		return err
	}
	err = os.Link(oldname, path)
	if os.IsNotExist(err) {
		return errNotReady
	}
	return err
}

func (l *Layouter) layoutSymLink(path string, md ztoc.FileMetadata) error {
	return os.Symlink(md.Linkname, path)
}
func (l *Layouter) layoutDir(path string, md ztoc.FileMetadata) error {
	return os.Mkdir(path, md.FileMode())
}
func (l *Layouter) layoutRegFile(path string, md ztoc.FileMetadata) error {
	dir, file := filepath.Split(path)
	if strings.HasPrefix(file, image.WhiteoutPrefix) {
		return l.layoutWhiteout(dir, file, md)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(int64(md.UncompressedSize))
}

func (l *Layouter) layoutWhiteout(dir, file string, md ztoc.FileMetadata) error {
	if file == image.WhiteoutOpaqueDir {
		return l.makeOpaque(dir)
	}
	file = strings.Replace(file, image.WhiteoutPrefix, "", 1)
	// todo: is this safe?
	path := filepath.Join(dir, file)
	md.Devmajor = 0
	md.Devminor = 0
	err := l.layoutCharDevice(path, md)
	if err != nil {
		return err
	}
	err = unix.Lsetxattr(path, "trusted.overlay.whiteout", []byte(image.OpaqueXattrValue), 0)
	if err != nil {
		return err
	}
	return errWhiteout
}

func (l *Layouter) makeOpaque(path string) error {
	err := unix.Lsetxattr(path, "trusted.overlay.opaque", []byte(image.OpaqueXattrValue), 0)
	if err != nil {
		return err
	}
	return errWhiteout
}

func (l *Layouter) layoutCharDevice(path string, md ztoc.FileMetadata) error {
	return l.layoutSpecial(unix.S_IFCHR, path, md)
}
func (l *Layouter) layoutBlockDevice(path string, md ztoc.FileMetadata) error {
	return l.layoutSpecial(unix.S_IFBLK, path, md)
}
func (l *Layouter) layoutFifo(path string, md ztoc.FileMetadata) error {
	return l.layoutSpecial(unix.S_IFIFO, path, md)
}

func (l *Layouter) layoutSpecial(ftype int64, path string, md ztoc.FileMetadata) error {
	mode := (md.Mode & 0o7777) | ftype
	return unix.Mknod(path, uint32(mode), int(unix.Mkdev(uint32(md.Devmajor), uint32(md.Devminor))))
}
