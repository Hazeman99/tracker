//go:build unix

package main

import "syscall"

// restrictUmask makes every file the process creates (database, WAL and SHM
// files) private to its owner.
func restrictUmask() { syscall.Umask(0o077) }
