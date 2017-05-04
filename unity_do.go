package main

import (
    "fmt"
	"log"
	"regexp"
	"strconv"
	"bufio"
	"time"
	"strings"

    "os"
	"os/exec"
    "path"
	"path/filepath"

    "github.com/hpcloud/tail"
)

func unityDo(ahkcmd *exec.Cmd, editorlog string, done chan<- int) {
	var reNothingChanged = regexp.MustCompile("^Refresh: detecting if any assets need to be imported or removed ... Refresh: elapses .* seconds \\(Nothing changed\\)")

	var reCompilerOutputStart = regexp.MustCompile("^-----CompilerOutput:-stdout")
	var reCompilerOutputEnd = regexp.MustCompile("^-----EndCompilerOutput")

	var reFinishedCompile = regexp.MustCompile("^- Finished compile")
	var reFailedCompile = regexp.MustCompile("^Compilation failed: ")
	var reEnlighten = regexp.MustCompile("^Enlighten scene contents: ")

	var reComputeAssetHashes = regexp.MustCompile("^----- Compute hash\\(es\\) for ([0-9]+) asset\\(s\\).")
	var reAssetSkipped = regexp.MustCompile("^----- Asset named (.*) is skipped as no actual change.")
	var reTotalAssetImport = regexp.MustCompile("^----- Total AssetImport time: ")

	counterHashed := 0
	counterSkipped := 0
	counterFinished := 0

	stateCompilerFailed := false
	stateCompilerOutputStarted := false
	stateNothingChanged := false

	var accCompilerMessages []string


	updateCompilerState := func(line string) {
		if reCompilerOutputStart.MatchString(line) {
			if ! stateCompilerOutputStarted {
				stateCompilerOutputStarted = true
				accCompilerMessages = nil
				stateCompilerFailed = false
			}
		}

		if stateCompilerOutputStarted {
			var re = regexp.MustCompile("^-----")
			if ! re.MatchString(line) {
				accCompilerMessages = append(accCompilerMessages, line)
			}
		}

		if reCompilerOutputEnd.MatchString(line) {
			stateCompilerOutputStarted = false
		}

		if reFailedCompile.MatchString(line) {
			stateCompilerFailed = true
		}
	}

	if file, err := os.Open(editorlog); err == nil {
		defer func() {
			err := file.Close()
			if err != nil {
				log.Fatal(err)
			}
		}()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			updateCompilerState(scanner.Text())
		}
	} else {
		log.Fatal(err)
	}

	endlocation := &tail.SeekInfo{Offset: 0, Whence: 2}
	if t, err := tail.TailFile(editorlog, tail.Config{Follow: true, Poll: true, Location: endlocation, Logger: tail.DiscardingLogger}); err == nil {
		err := ahkcmd.Start()
		if err != nil {
			log.Fatal(err)
		}

		tailing := false
		go func() {
			time.Sleep(2 * time.Second)
			if ! tailing {
				log.Print("Timeout! Is Unity minimized?")
				done <- 1
			}
		}()

		for line := range t.Lines {
			tailing = true

			updateCompilerState(line.Text)

			if reComputeAssetHashes.MatchString(line.Text) {
				s := reComputeAssetHashes.FindStringSubmatch(line.Text)[1]
				counterHashed, _ = strconv.Atoi(s)
			}

			if reAssetSkipped.MatchString(line.Text) {
				counterSkipped++
			}

			if reTotalAssetImport.MatchString(line.Text) {
				if counterHashed > 0 && counterHashed == counterSkipped {
					stateNothingChanged = true
				} else {
					counterSkipped = 0
					counterHashed = 0
				}
			}

			if reFinishedCompile.MatchString(line.Text) {
				if stateCompilerFailed {
					counterFinished = 3
				} else {
					counterFinished++
				}
			}

			if reEnlighten.MatchString(line.Text) {
				counterFinished = 2
			}

			if reNothingChanged.MatchString(line.Text) {
				stateNothingChanged = true
			}

			if stateNothingChanged || counterFinished >= 2 {
				for _, msg := range accCompilerMessages {
					fmt.Printf("%s\n", msg)
				}

				if stateCompilerFailed {
					done <- 1
				} else {
					done <- 0
				}

				return
			}
		}
	}

	done <- 1
}

func findCommandScript(cmd string) string {
	ret := cmd

	if _, err := os.Stat(ret); os.IsNotExist(err) {
		ret, err = exec.LookPath(cmd)
	}

	if _, err := os.Stat(ret); os.IsNotExist(err) {
		exe, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		cwd, err := filepath.Abs(filepath.Dir(exe))
		if err != nil {
			log.Fatal(err)
		}

		ret = path.Join(cwd, cmd)

		if _, err := os.Stat(ret); os.IsNotExist(err) {
			ret = path.Join(cwd, "unity_" + cmd + ".ahk")
		}
	}

	if _, err := os.Stat(ret); os.IsNotExist(err) {
		gopaths := strings.Split(os.Getenv("gopath"), ":;")
		for i := len(gopaths) - 1; i >= 0; i-- {
			testret := path.Join(gopaths[i], "src", "github.com", "rakete", "unity_do", cmd)
			if _, err := os.Stat(testret); err == nil {
				ret = testret
			}

			testret = path.Join(gopaths[i], "src", "github.com", "rakete", "unity_do", "unity_" + cmd + ".ahk")
			if _, err := os.Stat(testret); err == nil {
				ret = testret
			}
		}
	}

	return ret
}

func main() {
    userprofile := os.Getenv("userprofile")
    editorlog := path.Join(userprofile, "AppData", "Local", "Unity", "Editor", "Editor.log")
	programfiles := os.Getenv("programfiles")
	ahkexe := path.Join(programfiles, "AutoHotKey", "AutoHotKey.exe")

	if len(os.Args) < 2 {
		fmt.Printf("Usage: unity_do <command> <optional>")
		os.Exit(0)
	}

	ahkrunfirst := os.Args[1] //path.Join(userprofile, "Desktop", "unity_do", "unity_refresh.ahk")
	ahkrunfirst = findCommandScript(ahkrunfirst)
	ahkfirstcmd := exec.Command(ahkexe, ahkrunfirst)

	var ahkaftercmd *exec.Cmd
	if len(os.Args) > 2 {
		ahkrunafter := os.Args[2]
		ahkrunafter = findCommandScript(ahkrunafter)
		ahkaftercmd = exec.Command(ahkexe, ahkrunafter)
	}

	done := make(chan int)
	go unityDo(ahkfirstcmd, editorlog, done)

	exitcode := 0
	exitcode = <-done

	if exitcode == 0 && ahkaftercmd != nil {
		err := ahkaftercmd.Start()
		if err != nil {
			log.Fatal(err)
		}
	}

	os.Exit(exitcode)
}
