//go:build linux

package main

import "syscall"

func adviseSequential(data []byte) {
	_ = syscall.Madvise(data, syscall.MADV_SEQUENTIAL)
}
