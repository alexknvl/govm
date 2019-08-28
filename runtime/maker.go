package runtime

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"text/template"

	"github.com/lengzhao/govm/conf"
	"github.com/lengzhao/govm/counter"
)

// TDependItem app的依赖信息
type TDependItem struct {
	Alias   [4]byte
	AppName [32]byte
}

// TAppNewHead 新建app的头消息，不包含依赖列表
type TAppNewHead struct {
	LineNum   uint32
	Type      uint16
	Flag      uint8
	DependNum uint8
}

// TAppNewInfo 新建app的信息，不包含依赖列表
type TAppNewInfo struct {
	TAppNewHead
	Depends []TDependItem
}

const (
	// AppFlagRun the app can be call
	AppFlagRun = uint8(1 << iota)
	// AppFlagImport the app code can be included
	AppFlagImport
	// AppFlagPlublc App funds address uses the plublc address, except for app, others have no right to operate the address.
	AppFlagPlublc
	// AppFlagGzipCompress gzip compress
	AppFlagGzipCompress
)

// var envItems = []string{"GO111MODULE=on"}
var envItems = []string{}

// NewApp 创建app
func NewApp(chain uint64, name []byte, code []byte) {
	//1.生成原始文件，go build，校验是否正常
	//2.添加代码统计
	//3.如果可执行，添加执行代码

	nInfo := TAppNewInfo{}
	n := Decode(code, &nInfo.TAppNewHead)
	assert(nInfo.Type == 0)
	if nInfo.Flag >= 2*AppFlagGzipCompress {
		panic("error flag")
	}

	code = code[n:]
	for i := 0; i < int(nInfo.DependNum); i++ {
		item := TDependItem{}
		n := Decode(code, &item)
		code = code[n:]
		nInfo.Depends = append(nInfo.Depends, item)
	}
	if nInfo.Flag&AppFlagGzipCompress != 0 {
		buf := bytes.NewBuffer(code)
		var out bytes.Buffer
		zr, err := gzip.NewReader(buf)
		if err != nil {
			log.Fatal("gzip.NewReader", err)
		}
		if _, err := io.Copy(&out, zr); err != nil {
			log.Fatal("io.Copy", err)
		}

		if err := zr.Close(); err != nil {
			log.Fatal("zr.Close()", err)
		}
		code, err = ioutil.ReadAll(&out)
		if err != nil {
			log.Fatal("ioutil.ReadAll", err)
		}
	}

	appName := hexToPackageName(name)
	// srcFilePath := appName + ".go"
	filePath := GetFullPathOfApp(chain, name)
	dstFileName := path.Join(filePath, "app.go")

	srcFilePath := path.Join(projectRoot, "temp", fmt.Sprintf("chain%d", chain), "app.go")
	createDir(path.Dir(srcFilePath))
	createDir(path.Dir(dstFileName))
	// defer os.RemoveAll("temp")

	//判断源码是否已经存在，如果存在，则直接执行，返回有效代码行数
	//生成原始代码文件
	f, err := os.Create(srcFilePath)
	if err != nil {
		log.Println("fail to create go file:", srcFilePath, err)
		panic(err)
	}
	createSourceFile(chain, appName, nInfo.Depends, code, f)
	f.Close()
	//编译、校验原始代码
	cmd := exec.Command("go", "build", srcFilePath)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for _, item := range envItems {
		cmd.Env = append(cmd.Env, item)
	}

	err = cmd.Run()
	if err != nil {
		log.Println("fail to build source file:", srcFilePath, err)
		panic(err)
	}

	//为原始代码添加代码统计，生成目标带统计的代码文件
	lineNum := counter.Annotate(srcFilePath, dstFileName)
	if lineNum != uint64(nInfo.LineNum) {
		log.Println("error line number:", lineNum, ",hope:", nInfo.LineNum)
		panic(lineNum)
	}

	//再次编译，确认没有代码冲突
	cmd = exec.Command("go", "build", dstFileName)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for _, item := range envItems {
		cmd.Env = append(cmd.Env, item)
	}
	err = cmd.Run()
	if err != nil {
		log.Println("fail to build source file:", srcFilePath, err)
		panic(err)
	}

	if nInfo.Flag&AppFlagRun != 0 {
		makeAppExe(chain, name)
	}
	// os.RemoveAll("temp")
}

func createSourceFile(chain uint64, packName string, depends []TDependItem, code []byte, w io.Writer) {
	if bytes.Index(code, []byte("import")) != -1 {
		panic("code include 'import'")
	}

	if bytes.Index(code, []byte("_consume_tip_")) != -1 {
		panic("code include '_consume_tip_'")
	}

	w.Write([]byte("package "))
	w.Write([]byte(packName))
	w.Write([]byte("\n\n"))

	for _, item := range depends {
		realName := GetPackPath(chain, item.AppName[:])
		w.Write([]byte("import "))
		w.Write(item.Alias[:])
		w.Write([]byte(" \""))
		w.Write([]byte(realName))
		w.Write([]byte("\"\n"))
	}

	r := regexp.MustCompile("^// .build.*\n")
	code = r.ReplaceAll(code, []byte{})

	w.Write(code)
}

func hexToPackageName(in []byte) string {
	return "a" + hex.EncodeToString(in)
}

// TAppInfo app info
type TAppInfo struct {
	AppName  string
	PackPath string
	CorePath string
	ChainID  uint64
}

func makeAppExe(chain uint64, name []byte) {
	//1.add func GoVMRun
	//2.make func main
	//3.build
	//4.delete func GoVMRun
	c := conf.GetConf()
	packPath := GetPackPath(chain, name)
	corePath := GetPackPath(chain, c.CorePackName)
	info := TAppInfo{hexToPackageName(name), packPath, corePath, chain}
	s1, err := template.ParseFiles("run.tmpl")
	if err != nil {
		log.Println("fail to ParseFiles run.tmpl:", err)
		panic(err)
	}
	realPath := GetFullPathOfApp(chain, name)
	runFile := path.Join(realPath, "run.go")
	defer os.Remove(runFile)
	f, err := os.Create(runFile)
	if err != nil {
		log.Println("fail to create run file:", runFile, err)
		panic(err)
	}
	err = s1.Execute(f, info)
	if err != nil {
		log.Println("fail to execute run file:", runFile, err)
		f.Close()
		panic(err)
	}
	f.Close()
	// log.Println("create fun file:", runFile)
	fn := path.Join(projectRoot, "app_main", fmt.Sprintf("chain%d", chain), "main.go")
	exeFile := path.Join(projectRoot, "app_main", fmt.Sprintf("chain%d", chain), "app.exe")
	createDir(path.Dir(fn))

	fm, _ := os.Create(fn)
	defer fm.Close()
	s2, _ := template.ParseFiles("main.tmpl")
	s2.Execute(fm, info)

	//再次编译，确认没有代码冲突
	cmd := exec.Command("go", "build", "-o", exeFile, fn)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for _, item := range envItems {
		cmd.Env = append(cmd.Env, item)
	}
	err = cmd.Run()
	if err != nil {
		log.Println("fail to build source file:", fn, err)
		panic(err)
	}
	defer os.Remove(exeFile)
	//os.Chmod("app.exe", os.ModePerm)
	binFile := path.Join(realPath, "app.exe")
	os.Remove(binFile)
	os.Rename(exeFile, binFile)
}
