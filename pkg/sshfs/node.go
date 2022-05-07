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

var _ fs.NodeGetattrer = (*SFNode)(nil)

func (sn *SFNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	// logrus.WithField("Func", "Getattr").Debug("call")
	if f != nil {
		fr := f.(*sftp.File)
		stat, _ := fr.Stat()
		out.Mode = uint32(stat.Mode())
		out.Mtime = uint64(stat.ModTime().Unix())
		out.Ctime = uint64(stat.ModTime().Unix())
		out.Crtime_ = uint64(stat.ModTime().Unix())
		out.Size = uint64(stat.Size())
		// 正常な場合はfuse.OKを返す
		return fs.ToErrno(nil)
	}
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
	// logrus.WithField("Func", "Readdir").Debug("call")
	dirs := []fuse.DirEntry{}
	p := sn.RemotePath()
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
		out.Attr.Mode = cnode.Mode()
		return cnode, fs.ToErrno(nil)
	}
	p := sn.RemotePath()
	p = path.Join(p, name)
	f, err := sn.sftp.Stat(p)
	if err != nil {
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
	p = path.Join(p, name)
	if cnode.IsDir() {
		sn.sftp.RemoveDirectory(p)
		return fs.ToErrno(nil)
	}
	sn.sftp.Remove(p)
	sn.RmChild(name)
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
	out.Mode = newwNode.Mode()
	out.Ino = newwNode.StableAttr().Ino
	sn.AddChild(name, newwNode, false)
	p := sn.RemotePath()
	p = path.Join(p, name)
	err := sn.sftp.Mkdir(p)
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	err = sn.sftp.Chmod(p, iofs.FileMode(mode))
	if err != nil {
		return nil, fs.ToErrno(err)
	}

	return newwNode, fs.ToErrno(nil)
}

var _ fs.NodeOpendirer = (*SFNode)(nil)

func (sn *SFNode) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	logrus.WithField("Func", "Open").Debug(flags)
	p := sn.RemotePath()
	f, err := sn.sftp.OpenFile(p, int(flags))
	if err != nil {
		logrus.Debug(p)
		return nil, 0, fs.ToErrno(err)
	}
	return f, fuse.FOPEN_KEEP_CACHE | fuse.FOPEN_CACHE_DIR | fuse.FOPEN_STREAM, fs.ToErrno(nil)
}

func (sn *SFNode) Opendir(ctx context.Context) syscall.Errno {
	// logrus.WithField("Func", "Opendir").Debug("call")
	p := sn.RemotePath()
	file, err := sn.sftp.Open(p)
	if err != nil {
		return fs.ToErrno(err)
	}

	cnode := sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), fs.StableAttr{Mode: sn.StableAttr().Mode})
	sn.AddChild(file.Name(), cnode, false)

	return fs.ToErrno(nil)
}

var _ fs.NodeAccesser = (*SFNode)(nil)

func (sn *SFNode) Access(ctx context.Context, mask uint32) syscall.Errno {
	// logrus.WithField("Func", "Access").Debug("call")
	// p := sn.RemotePath()
	// stat, err := sn.sftp.Stat(p)
	// if err != nil {
	// 	return fs.ToErrno(err)
	// }
	// if uint32(stat.Mode().Perm())&mask == mask {
	// 	return fs.ToErrno(nil)
	// }
	// logrus.Warn(uint32(stat.Mode().Perm()), " ", mask)
	// return syscall.EPERM
	return fs.ToErrno(nil)
}

var _ fs.NodeCreater = (*SFNode)(nil)

func (sn *SFNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (node *fs.Inode, fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	logrus.WithField("Func", "Create").Debug(name)
	cnode := sn.GetChild(name)
	if cnode != nil {
		return cnode, cnode, 0, fs.ToErrno(nil)
	}
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
	stat, _ := newFile.Stat()
	out.Attr.Mode = uint32(stat.Mode())
	out.Attr.Mtime = uint64(stat.ModTime().Unix())
	out.Attr.Ctime = uint64(stat.ModTime().Unix())
	out.Attr.Crtime_ = uint64(stat.ModTime().Unix())
	out.Attr.Size = uint64(stat.Size())
	stable := fs.StableAttr{Mode: fuse.S_IFDIR}
	if !stat.IsDir() {
		stable.Mode = fuse.S_IFREG
	}
	newNode := sn.NewInode(ctx, NewSFNode(sn.sftp, sn.rootPath), stable)
	sn.AddChild(name, newNode, false)
	return newNode, newFile, 0, fs.ToErrno(err)
}

var _ fs.NodeReader = (*SFNode)(nil)

func (sn *SFNode) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	logrus.WithField("Func", "Read").Debug("offset ", off)
	var file *sftp.File
	if f != nil {
		file = f.(*sftp.File)
		logrus.Debug("READ: FileHandle was not empty: ", file)

	} else {
		var err error
		file, err = sn.sftp.Open(sn.RemotePath())
		if err != nil {
			return nil, fs.ToErrno(err)
		}
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
	logrus.WithField("Func", "Write").Debug(sn.RemotePath())
	var file *sftp.File
	if f != nil {
		logrus.Debug("READ: FileHandle was not empty")
		file = f.(*sftp.File)
	} else {
		var err error
		file, err = sn.sftp.Open(sn.RemotePath())
		if err != nil {
			return 0, fs.ToErrno(err)
		}
	}
	file.Seek(off, 0)
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
	p1 := path.Join(sn.RemotePath(), name)
	p2 := path.Join(sn.RemotePath(), newName)
	stable := sn.GetChild(name).StableAttr()
	sn.RmChild(name)
	newNode := sn.NewInode(ctx, newParent, stable)
	sn.AddChild(newName, newNode, true)

	err := sn.sftp.Rename(p1, p2)
	if err != nil {
		return fs.ToErrno(err)
	}
	return fs.ToErrno(err)
}

var _ fs.NodeSetattrer = (*SFNode)(nil)

func (sn *SFNode) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {

	out.Attr.Atime = in.Atime
	out.Attr.Atimensec = in.Atimensec
	out.Attr.Crtime_ = in.Crtime
	out.Attr.Crtimensec_ = in.Ctimensec
	out.Attr.Ctime = in.Ctime
	out.Attr.Ctimensec = in.Ctimensec
	out.Attr.Flags_ = in.Flags_
	if f != nil {
		file := f.(*sftp.File)
		file.Chmod(iofs.FileMode(in.Mode))
	}
	return fs.ToErrno(nil)
}

var _ fs.NodeUnlinker = (*SFNode)(nil)

func (sn *SFNode) Unlink(ctx context.Context, name string) syscall.Errno {
	p := sn.RemotePath()
	p = path.Join(p, name)
	sn.RmChild(name)
	err := sn.sftp.Remove(p)
	return fs.ToErrno(err)
}
