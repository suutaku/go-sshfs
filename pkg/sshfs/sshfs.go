package sshfs

import (
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
)

type Sshfs struct {
	sftp        *sftp.Client
	rootPath    string // remote path
	mountPoint  string // local mount point
	displayName string
}

func NewSshfs(sftp *sftp.Client, root, mountPoint, name string) *Sshfs {
	ret := &Sshfs{
		sftp:        sftp,
		rootPath:    root,
		mountPoint:  mountPoint,
		displayName: name,
	}
	return ret
}

func (sshfs *Sshfs) Mount(opts *fs.Options) error {
	if opts == nil {
		opts = &fs.Options{}
	}
	sec := time.Second
	opts.AttrTimeout = &sec
	opts.EntryTimeout = &sec
	opts.MountOptions.Options = append(opts.MountOptions.Options, "rw")
	opts.MountOptions.Options = append(opts.MountOptions.Options, "volname="+sshfs.displayName)
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
