package ziptool

import (
	"fmt"
	"testing"
)

func TestZip(t *testing.T) {
	if err := ZipFile(&[]string{"cfg/a.txt", "cfg/b.txt"}, "save/test.zip"); err != nil {
		fmt.Println(err)
	}

	if err := ZipDir("cfg/", "save/testDir.zip"); err != nil {
		fmt.Println(err)
	}
}
