package pct

import (
	"strings"

	"github.com/nu7hatch/gouuid"
)

type UUIDFactory interface {
	New() (string, error) // returns a new UUID
}

type UUID4 struct{}

func (u UUID4) New() (string, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	return strings.Replace(u4.String(), "-", "", 4), nil
}
