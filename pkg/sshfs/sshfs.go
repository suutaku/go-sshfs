package sshfs

import (
	"fmt"
	"runtime"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
)

type Sshfs struct {
	sftp        *sftp.Client
	rootPath    string // remote path
	mountPoint  string // local mount point
	displayName string
	server      *fuse.Server
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
	os := runtime.GOOS
	switch os {
	case "windows":
		fmt.Println("Windows")
	case "darwin":
		opts.MountOptions.Options = append(opts.MountOptions.Options, "volname="+sshfs.displayName)
	case "linux":
		opts.Name = sshfs.displayName
		opts.MountOptions.FsName = "ssh"
	default:
		fmt.Printf("%s.\n", os)
	}

	root := NewSFNode(sshfs.sftp, sshfs.rootPath)
	logrus.Debug("mount")
	server, err := fs.Mount(sshfs.mountPoint, root, opts)
	if err != nil {
		return err
	}
	sshfs.server = server
	logrus.Debug("serve")
	server.Wait()
	return err
}
func (sshfs *Sshfs) Unmount() error {
	if sshfs.server != nil {
		sshfs.server.Unmount()
	}
	return nil
}
