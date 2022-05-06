package sshfs

import (
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

type dirArray struct {
	entries []fuse.DirEntry
}

func (a *dirArray) HasNext() bool {
	return len(a.entries) > 0
}

func (a *dirArray) Next() (fuse.DirEntry, syscall.Errno) {
	e := a.entries[0]
	a.entries = a.entries[1:]
	return e, 0
}

func (a *dirArray) Close() {

}

// NewListDirStream wraps a slice of DirEntry as a DirStream.
func NewListDirStream(list []fuse.DirEntry) fs.DirStream {
	return &dirArray{list}
}
