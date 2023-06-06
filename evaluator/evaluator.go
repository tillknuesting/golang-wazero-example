package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	state := "0"
	if len(os.Args) > 1 {
		state = os.Args[1]
	}

	fmt.Println("From Args: ", state)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := scanner.Text()

		fmt.Println("From Stdin: ", input)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading standard input:", err)
	}
}
