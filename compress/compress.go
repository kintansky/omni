package ziptool

import (
	"archive/zip"
	"io/ioutil"
	"os"
	"strings"
)

// ZipFile 压缩一个或以上的文件，以切片的形式传入，可以不同目录
func ZipFile(fileList *[]string, savePath string) error {
	// 创建文件句柄用于写入
	zipFile, err := os.Create(savePath)
	if err != nil {
		return err
	}
	defer zipFile.Close()
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for _, file := range *fileList {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			return err
		}
		name := strings.Split(file, "/")
		f, err := zipWriter.Create(name[len(name)-1])
		if err != nil {
			return err
		}
		if _, err := f.Write(content); err != nil {
			return err
		}
	}
	return nil
}

func compress(file os.FileInfo, dirName string, zipWriter *zip.Writer) error {
	if file.IsDir() {
		// 文件夹的需要递归遍历
		dirName = dirName + file.Name() + "/"
		fileInfos, err := ioutil.ReadDir(dirName)
		if err != nil {
			return err
		}
		for _, f := range fileInfos {
			return compress(f, dirName, zipWriter)
		}
	} else {
		// 非文件夹直接写入
		fileName := dirName + file.Name()
		content, err := ioutil.ReadFile(fileName)
		if err != nil {
			return err
		}
		f, err := zipWriter.Create(fileName)
		if err != nil {
			return err
		}
		if _, err := f.Write(content); err != nil {
			return err
		}
	}
	return nil
}

// ZipDir 压缩整个目录，形参dirPath以/结尾
func ZipDir(dirPath string, savePath string) error {
	// 创建文件句柄用于写入
	zipFile, err := os.Create(savePath)
	if err != nil {
		return err
	}
	defer zipFile.Close()
	// 创建ZIP writer
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()
	// 遍历目录写入
	fileList, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return err
	}
	for _, file := range fileList {
		if err := compress(file, dirPath, zipWriter); err != nil {
			return err
		}
	}
	return nil
}
