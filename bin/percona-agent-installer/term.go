package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"strings"
)

type InvalidResponseError struct {
	Response string
}

func (e InvalidResponseError) Error() string {
	return e.Response
}

type Terminal struct {
	stdin *bufio.Reader
}

func NewTerminal(stdin io.Reader) *Terminal {
	t := &Terminal{
		stdin: bufio.NewReader(stdin),
	}
	return t
}

func (t *Terminal) PromptString(question string, defaultAnswer string) (string, error) {
	if defaultAnswer != "" {
		fmt.Printf("%s (%s): ", question, defaultAnswer)
	} else {
		fmt.Printf("%s: ", question)
	}
	bytes, _, err := t.stdin.ReadLine()
	if err != nil {
		return "", err
	}
	if Debug {
		log.Printf("raw answer='%s'\n", string(bytes))
	}
	answer := strings.TrimSpace(string(bytes))
	if answer == "" {
		answer = defaultAnswer
	}
	if Debug {
		log.Printf("final answer='%s'\n", answer)
	}
	return answer, nil
}

func (t *Terminal) PromptStringRequired(question string, defaultAnswer string) (string, error) {
	var answer string
	var err error
	for {
		answer, err = t.PromptString(question, defaultAnswer)
		if err != nil {
			return "", err
		}
		if answer == "" {
			fmt.Println(question + " is required, please try again")
			continue
		}
		return answer, nil
	}
}

func (t *Terminal) PromptBool(question string, defaultAnswer string) (bool, error) {
	for {
		answer, err := t.PromptString(question, defaultAnswer)
		if Debug {
			log.Printf("again=%t\n", answer)
			log.Printf("err=%s\n", err)
		}
		if err != nil {
			return false, err
		}
		answer = strings.ToLower(answer)
		switch answer {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			log.Println("Invalid response: '" + answer + "'.  Enter 'y' for yes, 'n' for no.")
			continue
		}
	}
}
