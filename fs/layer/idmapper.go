package layer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hanwen/go-fuse/v2/fuse"
)

var DefaultFuseIdMapper = FuseIdMapper{
	&IdMapper{0, 0, 0},
	&IdMapper{0, 0, 0},
}

func NewIdMapper(idmap string) (*IdMapper, error) {
	split := strings.Split(idmap, ":")
	if len(split) != 3 {
		return nil, fmt.Errorf("invalid id map: %s", idmap)
	}
	containerId, err := strconv.ParseUint(split[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid container id: %s", split[0])
	}
	hostId, err := strconv.ParseUint(split[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid host id: %s", split[1])
	}
	size, err := strconv.ParseUint(split[2], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid size: %s", split[3])
	}
	return &IdMapper{uint32(containerId), uint32(hostId), uint32(size)}, nil
}

func NewFuseIdMapper(uidmap, gidmap *IdMapper) *FuseIdMapper {
	return &FuseIdMapper{uidmap, gidmap}
}

type IdMapper struct {
	containerId uint32
	hostId      uint32
	length      uint32
}

func (i *IdMapper) Map(id uint32) uint32 {
	if id >= i.containerId && id < i.containerId+i.length {
		return id - i.containerId + i.hostId
	}
	return id
}

type FuseIdMapper struct {
	uidMapper *IdMapper
	gidMapper *IdMapper
}

func (f *FuseIdMapper) Map(attr *fuse.Attr) {
	attr.Uid = f.uidMapper.Map(attr.Uid)
	attr.Gid = f.uidMapper.Map(attr.Gid)
}
