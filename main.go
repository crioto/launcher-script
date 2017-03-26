package main

//import "golang.org/x/crypto/openpgp"
import (
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
)

type FileStatus int

const (
	FileDefault       FileStatus = 0
	FileMissingLocal  FileStatus = 1
	FileMissingRemote FileStatus = 2
	FileMismatch      FileStatus = 3
	FileActual        FileStatus = 4
)

type File struct {
	Hash     string
	File     string
	Filepath string
	Source   string // master/dev/production
	RemoteId string
	Status   FileStatus
}

type RemoteFile struct {
	Id    string   `json:"id"`
	Size  int      `json:"size"`
	Name  string   `json:"name"`
	Owner []string `json:"owner"`
}

type Gorjun struct {
	Codename    string
	Host        string
	RemoteFiles []RemoteFile
}

type LS struct {
	Hosts []Gorjun
	Files []File
}

func (l *LS) addFile(f File) {
	l.Files = append(l.Files, f)
}

func (l *LS) fileEntity(filepath string, file os.FileInfo, err error) error {
	base := path.Base(path.Dir(filepath))
	if base != "production" && base != "dev" && base != "master" {
		return nil
	}

	var f File
	f.File = file.Name()
	f.Filepath = filepath
	f.Source = base
	f.Status = FileDefault
	nf, err := os.Open(filepath)
	if err != nil {
		fmt.Printf("Failed to open %s: %v", filepath, err)
		return err
	}
	defer nf.Close()

	h := md5.New()
	if _, err := io.Copy(h, nf); err != nil {
		fmt.Printf("Failed to calculate hash for %s: %v", filepath, err)
		return err
	}
	f.Hash = fmt.Sprintf("%x", h.Sum(nil))

	l.addFile(f)

	return nil
}

func main() {
	ls := new(LS)

	hosts := [...]string{
		"devcdn.subut.ai:8338/kurjun/rest",
		"mastercdn.subut.ai:8338/kurjun/rest",
		"cdn.subut.ai:8338/kurjun/rest",
	}

	for _, h := range hosts {
		var g Gorjun
		g.Host = "https://" + h
		if h[0:3] == "dev" {
			g.Codename = "dev"
		} else if h[0:3] == "mas" {
			g.Codename = "master"
		} else {
			g.Codename = "production"
		}
		ls.Hosts = append(ls.Hosts, g)
	}

	fmt.Printf("Gathering information about local files\n")

	flag.Parse()
	topDirectory := flag.Arg(0)

	err := filepath.Walk(topDirectory, ls.fileEntity)
	if err != nil {
		fmt.Printf("Directory iteration failed: %v", err)
	}

	fmt.Printf("Retrieve files from gorjun\n")
	for i, h := range ls.Hosts {
		resp, err := http.Get(fmt.Sprintf("%s/raw/info", h.Host))
		if err != nil {
			fmt.Printf("Failed to retrieve file list from %s", h.Host)
			continue
		}
		data, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Printf("Failed to read body from %s", h.Host)
			continue
		}

		var rf []RemoteFile
		err = json.Unmarshal(data, &rf)
		if err != nil {
			fmt.Printf("Failed to unmarshal contents from %s", h.Host)
			continue
		}
		ls.Hosts[i].RemoteFiles = rf
	}

	fmt.Printf("Local files: %d\n", len(ls.Files))
	for _, h := range ls.Hosts {
		fmt.Printf("%d files in %s kurjun\n", len(h.RemoteFiles), h.Codename)
	}

	for i, f := range ls.Files {
		for _, h := range ls.Hosts {
			if f.Source != h.Codename {
				continue
			}
			for _, r := range h.RemoteFiles {
				if r.Name == f.File {
					ls.Files[i].RemoteId = r.Id
					if r.Id == f.Hash {
						ls.Files[i].Status = FileActual
					} else {
						ls.Files[i].Status = FileMismatch
					}
				}
			}
		}
		if ls.Files[i].Status == FileDefault {
			ls.Files[i].Status = FileMissingRemote
		}
	}

	for _, h := range ls.Hosts {
		fmt.Printf("Retrieving token for %s\n", h.Codename)
		taction := exec.Command("./kurjun.sh", "token", h.Host, fmt.Sprintf("%s-ktoken", h.Codename), "launcher", "i@crioto.com")
		err = taction.Run()
		if err != nil {
			fmt.Printf("Token action failed: %v\n", err)
			return
		}
	}

	for _, f := range ls.Files {
		fmt.Printf("File: %s/%s Status: %d\n", f.Source, f.File, int(f.Status))
		host := ""
		for _, h := range ls.Hosts {
			if f.Source == h.Codename {
				host = h.Host
			}
		}
		if host == "" {
			continue
		}
		if f.Status == FileMissingRemote {
			fmt.Printf("Uploading new file %s to %s[%s]\n", f.File, f.Source, host)
			action := exec.Command("./kurjun.sh", "upload", host, f.Filepath, "launcher", "i@crioto.com", f.Source)
			err := action.Run()
			if err != nil {
				fmt.Printf("Action failed: %v\n", err)
				continue
			}
		} else if f.Status == FileMismatch {
			fmt.Printf("Modifying file %s to %s\n", f.File, f.Source)
			raction := exec.Command("./kurjun.sh", "delete", host, f.File, "launcher", "i@crioto.com", f.Source)
			err := raction.Run()
			if err != nil {
				fmt.Printf("Action 1 failed: %v\n", err)
				continue
			}
			uaction := exec.Command("./kurjun.sh", "upload", host, f.Filepath, "launcher", "i@crioto.com", f.Source)
			err = uaction.Run()
			if err != nil {
				fmt.Printf("Action 2 failed: %v\n", err)
				continue
			}
		}
	}

}
