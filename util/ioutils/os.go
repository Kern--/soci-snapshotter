package ioutils

import "os"

func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
