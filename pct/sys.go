package pct

import (
	"os"
)

func FileSize(fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

func SameFile(file1, file2 string) (bool, error) {
	var err error

	stat1, err := os.Stat(file1)
	if err != nil {
		return false, err
	}

	stat2, err := os.Stat(file2)
	if err != nil {
		return false, err
	}

	return os.SameFile(stat1, stat2), nil
}
