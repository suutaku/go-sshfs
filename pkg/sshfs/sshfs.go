package sshfs

import (
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
)

type Sshfs struct {
	root       SFNode
	sftp       *sftp.Client
	rootPath   string // remote path
	mountPoint string // local mount point
	localPath  string
}

func NewSshfs(sftp *sftp.Client, root, mountPoint string) *Sshfs {
	ret := &Sshfs{
		sftp:       sftp,
		rootPath:   root,
		mountPoint: mountPoint,
	}
	return ret
}

func (sshfs *Sshfs) Mount(opts *fs.Options) error {
	if opts == nil {
		opts = &fs.Options{}
	}
	root := NewSFNode(sshfs.sftp, sshfs.rootPath)
	logrus.Debug("mount")
	server, err := fs.Mount(sshfs.mountPoint, root, opts)
	if err != nil {
		return err
	}
	logrus.Debug("serve")
	server.Wait()
	return nil
}
func (sssnhfs *Sshfs) Unmount() error {
	return nil
}
