package main

import (
	"bufio"
	"fmt"
	"os"
)

func AskUserWitDefaultAnswer(question string, defaultAnswer string) (answer string, err error) {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf(question+" [default: %s]: ", defaultAnswer)
	scanner.Scan()
	if err := scanner.Err(); err != nil {
		return "", err
	}
	answer = scanner.Text()
	if answer == "" {
		answer = defaultAnswer
	}

	return answer, nil
}

func AskUser(question string) (answer string, err error) {
	scanner := bufio.NewScanner(os.Stdin)

	for answer == "" {
		fmt.Print(question + ": ")
		scanner.Scan()
		if err := scanner.Err(); err != nil {
			return "", err
		}
		answer = scanner.Text()
	}

	return answer, nil
}
