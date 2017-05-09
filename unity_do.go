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

type stateNode int

const (
	null stateNode = iota
	compileGame
	outputGame
	failingGame
	finishedGame
	compileEditor
	outputEditor
	failingEditor
	success
	failed
	hashskip
	refresh
)

type compilerState struct {
	counterHashed int
	counterSkipped int

	node stateNode
	messages []string

	reRefresh *regexp.Regexp

	reComputeAssetHashes *regexp.Regexp
	reAssetSkipped *regexp.Regexp
	reTotalAssetImport *regexp.Regexp

	reStartingCompile *regexp.Regexp
	reCompilerOutputStart *regexp.Regexp
	reFailedCompile *regexp.Regexp
	reCompilerOutputEnd *regexp.Regexp
	reFinishedCompile *regexp.Regexp
}

func initState() compilerState {
	var ret compilerState

	ret.counterHashed = 0
	ret.counterSkipped = 0

	ret.node = null
	ret.messages = nil

	ret.reRefresh = regexp.MustCompile("^Refresh: detecting if any assets need to be imported or removed ... Refresh: elapses .* seconds \\(Nothing changed\\)")

	ret.reComputeAssetHashes = regexp.MustCompile("^----- Compute hash\\(es\\) for ([0-9]+) asset\\(s\\).")
	ret.reAssetSkipped = regexp.MustCompile("^----- Asset named (.*) is skipped as no actual change.")
	ret.reTotalAssetImport = regexp.MustCompile("^----- Total AssetImport time: ")

	ret.reStartingCompile = regexp.MustCompile("^- starting compile")
	ret.reCompilerOutputStart = regexp.MustCompile("^-----CompilerOutput:-stdout")
	ret.reFailedCompile = regexp.MustCompile("^Compilation failed: ")
	ret.reCompilerOutputEnd = regexp.MustCompile("^-----EndCompilerOutput")
	ret.reFinishedCompile = regexp.MustCompile("^- Finished compile")

	return ret
}

func updateState(line string, state compilerState) compilerState {

	if state.reRefresh.MatchString(line) && state.node == null {
		state.node = refresh
	} else if state.reComputeAssetHashes.MatchString(line) && (state.node == null || state.node == refresh)  {
		state.node = hashskip
		state.counterHashed = 0
		state.counterSkipped = 0

		s := state.reComputeAssetHashes.FindStringSubmatch(line)[1]
		state.counterHashed, _ = strconv.Atoi(s)
	} else if state.reAssetSkipped.MatchString(line) && state.node == hashskip {
		state.counterSkipped++
	} else if state.reTotalAssetImport.MatchString(line) && state.node == hashskip {
		if state.counterHashed > 0 && state.counterHashed == state.counterSkipped {
			state.node = refresh
		}
	} else if state.reStartingCompile.MatchString(line) && (state.node == null || state.node == refresh || state.node == hashskip || state.node == failed || state.node == success) {
		state.node = compileGame

		state.messages = nil
	} else if state.reCompilerOutputStart.MatchString(line) && state.node == compileGame {
		state.node = outputGame
	} else if state.reFailedCompile.MatchString(line) && state.node == outputGame {
		state.node = failingGame
	} else if state.reCompilerOutputEnd.MatchString(line) && (state.node == outputGame || state.node == failingGame) {
		if state.node == outputGame {
			state.node = finishedGame
		} else if state.node == failingGame {
			state.node = failed
		}
	} else if state.reFinishedCompile.MatchString(line) && state.node == compileGame {
		state.node = finishedGame
	} else if state.reStartingCompile.MatchString(line) && state.node == finishedGame {
		state.node = compileEditor
	} else if state.reCompilerOutputStart.MatchString(line) && state.node == compileEditor {
		state.node = outputEditor
	} else if state.reFailedCompile.MatchString(line) && state.node == outputEditor {
		state.node = failingEditor
	} else if state.reCompilerOutputEnd.MatchString(line) && (state.node == outputEditor || state.node == failingEditor) {
		if state.node == outputEditor {
			state.node = success
		} else if state.node == failingEditor {
			state.node = failed
		}
	} else if state.reFinishedCompile.MatchString(line) && state.node == compileEditor {
		state.node = success
	}

	if state.node == outputGame || state.node == outputEditor || state.node == failingGame || state.node == failingEditor {
		var re = regexp.MustCompile("^-----")
		if ! re.MatchString(line) {
			state.messages = append(state.messages, line)
		}
	}

	return state
}

func printState(state compilerState) {
	for _, line := range state.messages {
		fmt.Printf("%s\n", line)
	}

	switch state.node {
		case null: fmt.Printf("null\n")
		case compileGame: fmt.Printf("compileGame\n")
		case outputGame: fmt.Printf("outputGame\n")
		case failingGame: fmt.Printf("failingGame\n")
		case finishedGame: fmt.Printf("finishedGame\n")
		case compileEditor: fmt.Printf("compileEditor\n")
		case outputEditor: fmt.Printf("outputEditor\n")
		case failingEditor: fmt.Printf("failingEditor\n")
		case success: fmt.Printf("success\n")
		case failed: fmt.Printf("failed\n")
		case hashskip: fmt.Printf("hashskip\n")
		case refresh: fmt.Printf("refresh\n")
	}
}

func unityDo(ahkcmdlist []*exec.Cmd, waitms time.Duration, editorlog string, done chan<- int) {

	state := initState()
	oldstate := state

	if file, err := os.Open(editorlog); err == nil {
		defer func() {
			err := file.Close()
			if err != nil {
				log.Fatal(err)
			}
		}()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			oldstate = updateState(scanner.Text(), oldstate)
		}
	} else {
		log.Fatal(err)
	}

	endlocation := &tail.SeekInfo{Offset: 0, Whence: 2}
	if t, err := tail.TailFile(editorlog, tail.Config{Follow: true, Poll: true, Location: endlocation, Logger: tail.DiscardingLogger}); err == nil {
		tailing := false
		go func() {
			for _, ahkcmd := range ahkcmdlist {
				if ! tailing {
					err = ahkcmd.Run()
					if err != nil { log.Fatal(err) }
				}

				time.Sleep(waitms * time.Millisecond)
			}

			if ! tailing {
				log.Print("Timeout! Is Unity minimized?")
				done <- 1
			}
		}()

		for line := range t.Lines {
			tailing = true

			state = updateState(line.Text, state)

			if state.node == success || state.node == failed {
				printState(state)

				if state.node == failed {
					done <- 1
				} else {
					done <- 0
				}

				return
			}

			if state.node == refresh {
				if oldstate.node == success || oldstate.node == failed {
					printState(oldstate)
				}

				if oldstate.node == failed {
					done <- 1
				} else {
					done <- 0
				}
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
	programfilesx86 := os.Getenv("programfiles(x86")

	ahkexe, _ := exec.LookPath("AutoHotKey.exe")
	if _, err := os.Stat(ahkexe); os.IsNotExist(err) {
		ahkexe = path.Join(programfiles, "AutoHotKey", "AutoHotKey.exe")
	}

	if _, err := os.Stat(ahkexe); os.IsNotExist(err) {
		ahkexe = path.Join(programfilesx86, "AutoHotKey", "AutoHotKey.exe")
	}

	if _, err := os.Stat(ahkexe); os.IsNotExist(err) {
		log.Fatal("AutoHotKey.exe not found, please make sure you have AutoHotKey installed.")
	}

	if len(os.Args) < 2 {
		fmt.Printf("Usage: unity_do <command> <optional command to run after first>\n")
		fmt.Printf("A command may be any AutoHotKey script, or refresh, build or play.\n")
		os.Exit(0)
	}

	ahkrunfirst := os.Args[1]
	ahkrunfirst = findCommandScript(ahkrunfirst)
	var ahkfirstlist []*exec.Cmd
	ahkfirstlist = append(ahkfirstlist, exec.Command(ahkexe, ahkrunfirst))
	ahkfirstlist = append(ahkfirstlist, exec.Command(ahkexe, ahkrunfirst))
	ahkfirstlist = append(ahkfirstlist, exec.Command(ahkexe, ahkrunfirst))

	var ahkaftercmd *exec.Cmd
	if len(os.Args) > 2 {
		ahkrunafter := os.Args[2]
		ahkrunafter = findCommandScript(ahkrunafter)
		ahkaftercmd = exec.Command(ahkexe, ahkrunafter)
	}

	done := make(chan int)
	go unityDo(ahkfirstlist, 500, editorlog, done)

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
