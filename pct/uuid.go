package pct

import (
	"fmt"
	"strings"

	"github.com/nu7hatch/gouuid"
)

type UUIDFactoryInterface interface {
	New() string
}

type UUID struct{}

func (u UUID) New() string {
	u4, err := uuid.NewV4()
	if err != nil {
		fmt.Println("Could not create UUID4: %v", err)
		return ""
	}
	return strings.Replace(u4.String(), "-", "", -1)
}

func NewUUIDFactory() UUID {
	return UUID{}
}
