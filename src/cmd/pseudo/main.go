package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"pseudo/internal/interpreter"
)

const version = "1.0.0"

const banner = `
  ██████╗ ███████╗███████╗██╗   ██╗██████╗  ██████╗
  ██╔══██╗██╔════╝██╔════╝██║   ██║██╔══██╗██╔═══██╗
  ██████╔╝███████╗█████╗  ██║   ██║██║  ██║██║   ██║
  ██╔═══╝ ╚════██║██╔══╝  ██║   ██║██║  ██║██║   ██║
  ██║     ███████║███████╗╚██████╔╝██████╔╝╚██████╔╝
  ╚═╝     ╚══════╝╚══════╝ ╚═════╝ ╚═════╝  ╚═════╝

  The language that speaks your language.
  v%s  |  Type 'help' for commands, 'exit' to quit
`

const helpText = `
  Pseudo Language - Quick Reference
  ──────────────────────────────────────────────────────

  VARIABLES
    name = "Lepri"
    scores = [85, 42, 91]

  CONDITIONS
    check if age >= 18
    if yes → say "adult"
    else → say "minor"

  LOOPS & FILTERING
    go in list
    find each item where score >= 50
    remove from list where score < 50
    for each item in list
        print item
    done

  MATH
    divide number by 2 / if remainder = 0 → say "even"
    add all prices together / print total
    compare numbers / print biggest
    increase score by 10
    calculate 10 * 5 + 3 into result

  STRINGS
    make name uppercase / lowercase / trimmed
    replace "old" with "new" in text
    split text by " " into words
    join words with "-" into result
    get first 5 characters of text into short

  FUNCTIONS
    define greet with name
        say "Hello"
        print name
    done
    run greet with "Lepri"

  REPEAT
    repeat 5 times
        say "hello"
    done

  OBJECTS
    make object user with name: "Lepri", age: 21
    set user.city to "Skopje"
    get user.name into name

  DATE / TIME
    get today into date
    get year into yr

  HTTP
    fetch "https://api.example.com/data" into response

  FILE I/O
    save data to "output.txt"
    load content from "file.txt"

  ──────────────────────────────────────────────────────
`

func runFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("\n  [Pseudo] File not found: '%s'\n\n", path)
		os.Exit(1)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("\n  [Pseudo] Couldn't read file: %v\n\n", err)
		os.Exit(1)
	}
	interp := interpreter.New()
	interp.Execute(string(data))
}

func runREPL() {
	fmt.Printf(banner, version)

	interp := interpreter.New()
	scanner := bufio.NewScanner(os.Stdin)
	var buffer []string
	inBlock := false

	blockStarters := []string{"define ", "repeat ", "for each "}
	endKeyword := "done"

	for {
		if inBlock {
			fmt.Print("  ... ")
		} else {
			fmt.Print("  >>> ")
		}

		if !scanner.Scan() {
			fmt.Println("\n\n  Goodbye.\n")
			break
		}

		line := scanner.Text()
		stripped := strings.TrimSpace(line)

		switch strings.ToLower(stripped) {
		case "exit", "quit":
			fmt.Println("\n  Goodbye.\n")
			return
		case "help":
			fmt.Println(helpText)
			continue
		case "clear":
			interp = interpreter.New()
			fmt.Println("  [cleared]\n")
			continue
		case "vars":
			hasVars := false
			for k, v := range interp.Variables {
				if !strings.HasPrefix(k, "_") {
					if !hasVars {
						fmt.Println()
					}
					fmt.Printf("    %s = %v\n", k, v)
					hasVars = true
				}
			}
			if !hasVars {
				fmt.Println("  [no variables set]")
			}
			fmt.Println()
			continue
		}

		for _, starter := range blockStarters {
			if strings.HasPrefix(stripped, starter) {
				inBlock = true
				break
			}
		}

		buffer = append(buffer, line)

		if stripped == endKeyword {
			inBlock = false
		}

		if !inBlock {
			code := strings.Join(buffer, "\n")
			buffer = nil
			if strings.TrimSpace(code) != "" {
				interp.Execute(code)
				fmt.Println()
			}
		}
	}
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		runREPL()
		return
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Println(helpText)
	case "-v", "--version":
		fmt.Printf("  Pseudo v%s\n", version)
	default:
		runFile(args[0])
	}
}
