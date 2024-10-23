package image

import (
	"path/filepath"
	"strings"
)

const (
	WhiteoutPrefix    = ".wh."
	WhiteoutOpaqueDir = WhiteoutPrefix + WhiteoutPrefix + ".opq"
	OpaqueXattrValue  = "y"
)


func IsWhiteout(path string) bool {
	_, file := filepath.Split(path)
	return strings.HasPrefix(file, WhiteoutPrefix)

}

