package sshfs

import (
	"context"
	"os"
	"path"
	"syscall"

	iofs "io/fs"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
)

type SFNode struct {
	fs.Inode
	sftp     *sftp.Client
	rootPath string
}

func (sn *SFNode) Name() string {
	return path.Base(sn.Path(nil))
}

func (sn *SFNode) LocalPath() string {
	return sn.Path(nil)
}

func (sn *SFNode) RemotePath() string {
	return path.Join(sn.rootPath, sn.Path(nil))
}

func NewSFNode(sftp *sftp.Client, root string) *SFNode {
	ret := &SFNode{
		sftp:     sftp,
		rootPath: root,
	}
	return ret
}

func copyAttr(dest *fuse.Attr, attr fs.StableAttr) {
	dest.Mode = attr.Mode
	dest.Ino = attr.Ino
}

var _ fs.NodeGetattrer = (*SFNode)(nil)

func (sn *SFNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	// logrus.WithField("Func", "Getattr").Debug("call")
	path := sn.RemotePath()
	stat, err := sn.sftp.Stat(path)
	if err != nil {
		return fs.ToErrno(err)
	}
	statT, ok := stat.Sys().(*sftp.FileStat)
	if ok {
		out.Atime = uint64(statT.Atime)
	}
	out.Mode = uint32(stat.Mode())
	out.Mtime = uint64(stat.ModTime().Unix())
	out.Ctime = uint64(stat.ModTime().Unix())
	out.Crtime_ = uint64(stat.ModTime().Unix())
	out.Size = uint64(stat.Size())
	// 正常な場合はfuse.OKを返す
	return fs.ToErrno(nil)
}

var _ fs.NodeReaddirer = (*SFNode)(nil)

// ReadDirAll returns a list of sshfs
func (sn *SFNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	logrus.WithField("Func", "Readdir").Debug("call")
	dirs := []fuse.DirEntry{}
	p := sn.RemotePath()
	logrus.Warn("handling Dir.ReadDirAll call stfp read dir", p)
	fss, err := sn.sftp.ReadDir(p)
	if err != nil {
		return NewListDirStream(dirs), fs.ToErrno(err)
	}
	for _, f := range fss {
		// t := fuse.DT_File
		childnode := sn.GetChild(f.Name())
		if childnode == nil {
			stable := fs.StableAttr{Mode: fuse.S_IFDIR}
			if !f.IsDir() {
				stable.Mode = fuse.S_IFREG
			}
			childnode = sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), stable)
			sn.AddChild(f.Name(), childnode, false)
		}
		d := fuse.DirEntry{
			Name: f.Name(),
			Ino:  childnode.StableAttr().Ino,
			Mode: childnode.Mode(),
		}
		dirs = append(dirs, d)
	}
	return NewListDirStream(dirs), fs.ToErrno(nil)
}

var _ fs.NodeLookuper = (*SFNode)(nil)

func (sn *SFNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	// logrus.WithField("Func", "Lookup").Debug(name)
	cnode := sn.GetChild(name)
	if cnode != nil { // Local exist
		return cnode, fs.ToErrno(nil)
	}
	p := sn.RemotePath()
	f, err := sn.sftp.Stat(p)
	if err != nil {
		if err == os.ErrNotExist {
			return nil, fs.ToErrno(err)
		}
		return nil, fs.ToErrno(err)
	}
	out.Attr.Mode = uint32(f.Mode())
	out.Attr.Mtime = uint64(f.ModTime().Unix())
	out.Attr.Ctime = uint64(f.ModTime().Unix())
	out.Attr.Crtime_ = uint64(f.ModTime().Unix())
	out.Attr.Size = uint64(f.Size())
	stable := fs.StableAttr{Mode: fuse.S_IFDIR}
	if !f.IsDir() {
		stable.Mode = fuse.S_IFREG
	}
	cnode = sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), stable)
	sn.AddChild(f.Name(), cnode, false)
	return cnode, fs.ToErrno(nil)
}

var _ fs.NodeRmdirer = (*SFNode)(nil)

func (sn *SFNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	logrus.WithField("Func", "Rmdir").Debug(name)
	cnode := sn.GetChild(name)
	if cnode == nil {
		return fs.ToErrno(nil)
	}
	p := sn.RemotePath()
	if cnode.IsDir() {
		sn.sftp.RemoveDirectory(p)
		return fs.ToErrno(nil)
	}
	sn.sftp.Remove(p)
	return fs.ToErrno(nil)
}

var _ fs.NodeMkdirer = (*SFNode)(nil)

func (sn *SFNode) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	logrus.WithField("Func", "Mkdir").Debug(name)
	cnode := sn.GetChild(name)
	if cnode != nil {
		return nil, syscall.Errno(fuse.EBUSY)
	}

	newwNode := sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), fs.StableAttr{Mode: mode})
	sn.AddChild(name, newwNode, false)
	p := sn.RemotePath()
	err := sn.sftp.Mkdir(path.Join(p, name))
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	err = sn.sftp.Chmod(path.Join(p, name), iofs.FileMode(mode))
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	copyAttr(&out.Attr, newwNode.StableAttr())
	return nil, fs.ToErrno(nil)
}

var _ fs.NodeOpendirer = (*SFNode)(nil)

func (sn *SFNode) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	logrus.WithField("Func", "Open").Debug("call")
	p := sn.RemotePath()
	f, err := sn.sftp.OpenFile(p, int(flags))
	if err != nil {
		return nil, 0, fs.ToErrno(err)
	}
	return f, 0, fs.ToErrno(nil)
}

func (sn *SFNode) Opendir(ctx context.Context) syscall.Errno {
	logrus.WithField("Func", "Opendir").Debug("call")
	p := sn.RemotePath()
	logrus.Warn("call open dir ", p)
	file, err := sn.sftp.Open(p)
	if err != nil {
		return fs.ToErrno(err)
	}

	cnode := sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), fs.StableAttr{Mode: sn.StableAttr().Mode})
	sn.AddChild(file.Name(), cnode, false)

	return fs.ToErrno(nil)
}

func (sn *SFNode) Access(mode uint32, context *fuse.Context) (code fuse.Status) {
	logrus.WithField("Func", "Access").Debug("call")
	return fuse.OK
}

var _ fs.NodeCreater = (*SFNode)(nil)

func (sn *SFNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (node *fs.Inode, fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	logrus.WithField("Func", "Create").Debug(name)
	cnode := sn.GetChild(name)
	if cnode != nil {
		return cnode, cnode, 0, fs.ToErrno(nil)
	}
	newNode := sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), fs.StableAttr{Mode: mode})
	sn.AddChild(name, newNode, false)
	p := sn.RemotePath()
	p = path.Join(p, name)
	newFile, err := sn.sftp.Create(p)
	if err != nil {
		return nil, nil, 0, fs.ToErrno(err)
	}
	err = sn.sftp.Chmod(p, os.FileMode(mode))
	if err != nil {
		return nil, nil, 0, fs.ToErrno(err)
	}
	return newNode, newFile, 0, fs.ToErrno(err)
}

var _ fs.NodeReader = (*SFNode)(nil)

func (sn *SFNode) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	logrus.WithField("Func", "Read").Debug("call")
	fr := f.(*sftp.File)
	info, err := fr.Stat()
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	logrus.Warn("read ", fr.Name())
	logrus.Warn("mode ", info.Mode())
	file, err := sn.sftp.OpenFile(sn.RemotePath(), int(info.Mode()))
	if err != nil {
		return nil, fs.ToErrno(err)
	}

	file.Seek(off, 0)
	n, err := file.Read(dest)
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	return fuse.ReadResultData(dest[:n]), fs.OK
}

var _ fs.NodeWriter = (*SFNode)(nil)

func (sn *SFNode) Write(ctx context.Context, f fs.FileHandle, data []byte, off int64) (written uint32, errno syscall.Errno) {
	file, err := sn.sftp.OpenFile(sn.RemotePath(), int(sn.StableAttr().Mode))
	if err != nil {
		return 0, fs.ToErrno(err)
	}
	n, err := file.Write(data)
	return uint32(n), fs.ToErrno(err)
}

var _ fs.NodeFsyncer = (*SFNode)(nil)

func (sn *SFNode) Fsync(ctx context.Context, f fs.FileHandle, flags uint32) syscall.Errno {
	return fs.ToErrno(nil)
}

var _ fs.NodeReleaser = (*SFNode)(nil)

func (sn *SFNode) Release(ctx context.Context, f fs.FileHandle) syscall.Errno {
	fr := f.(*sftp.File)
	err := fr.Close()
	return fs.ToErrno(err)
}

var _ fs.NodeRenamer = (*SFNode)(nil)

func (sn *SFNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	p1 := path.Join(sn.LocalPath(), name)
	p2 := path.Join(sn.LocalPath(), newParent.EmbeddedInode().Path(nil), newName)

	err := syscall.Rename(p1, p2)
	if err != nil {
		return fs.ToErrno(err)
	}
	err = sn.sftp.Rename(p1, p2)
	return fs.ToErrno(err)
}
