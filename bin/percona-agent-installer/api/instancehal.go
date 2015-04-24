package api

import (
	"github.com/jagregory/halgo"
	"github.com/percona/cloud-protocol/proto/v2"
)

type InstanceHAL struct {
	halgo.Links
	proto.Instance
}
